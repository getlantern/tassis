package redisbroker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getlantern/tassis/util"
	"github.com/go-redis/redis/v8"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishSubscribe(t *testing.T) {
	topic := fmt.Sprintf("%d", util.NowUnixMillis())

	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	defer client.Close()

	// clear the database
	keys, err := client.Keys(context.Background(), "*").Result()
	require.NoError(t, err)
	if len(keys) > 0 {
		err = client.Del(context.Background(), keys...).Err()
		require.NoError(t, err)
	}

	broker, err := New(client)
	require.NoError(t, err)
	t.Run("concurrent subscribers get all messages, send acks", func(t *testing.T) {
		var wg sync.WaitGroup

		ackers := make([]func() error, 0)
		var ackersMx sync.Mutex
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				sub, err := broker.NewSubscriber(topic)
				if !assert.NoError(t, err) {
					return
				}
				defer sub.Close()

				i := 0
				var acker func() error
			items:
				for {

					select {
					case msg := <-sub.Messages():
						if !assert.Equal(t, fmt.Sprintf("msg%d", i), string(msg.Data())) {
							return
						}
						acker = msg.Acker()
						i++
					case <-time.After(1 * time.Second):
						break items
					}
				}
				ackersMx.Lock()
				ackers = append(ackers, acker)
				ackersMx.Unlock()
				assert.Equal(t, 100, i)
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			time.Sleep(250 * time.Millisecond)
			pub, err := broker.NewPublisher(topic)
			if !assert.NoError(t, err) {
				return
			}

			defer pub.Close()

			for i := 0; i < 100; i++ {
				err := pub.Publish([]byte(fmt.Sprintf("msg%d", i)))
				if !assert.NoError(t, err) {
					return
				}
			}
		}()

		wg.Wait()

		wg.Add(len(ackers))
		for _, a := range ackers {
			acker := a
			go func() {
				defer wg.Done()
				err := acker()
				assert.NoError(t, err)
			}()
		}
		wg.Wait()
	})

	t.Run("next subscriber gets no messages", func(t *testing.T) {
		sub, err := broker.NewSubscriber(topic)
		require.NoError(t, err)
		defer sub.Close()

		i := 0
	items:
		for {
			select {
			case <-sub.Messages():
				i++
			case <-time.After(250 * time.Millisecond):
				break items
			}
		}
		require.Equal(t, 0, i)
	})

	t.Run("trim acks then clear offsets and next subscriber gets no messages", func(t *testing.T) {
		err := TrimStreams(client, 1000, 1)
		require.NoError(t, err)

		offsetKeys, err := client.Keys(context.Background(), "offset:*").Result()
		require.NoError(t, err)

		if len(offsetKeys) > 0 {
			err = client.Del(context.Background(), offsetKeys...).Err()
			require.NoError(t, err)
		}

		sub, err := broker.NewSubscriber(topic)
		require.NoError(t, err)
		defer sub.Close()

		i := 0
	items:
		for {
			select {
			case <-sub.Messages():
				i++
			case <-time.After(250 * time.Millisecond):
				break items
			}
		}
		require.Equal(t, 0, i)
	})
}
