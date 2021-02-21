package redisbroker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishSubscribe(t *testing.T) {
	topic := fmt.Sprintf("%d", time.Now().UnixNano())

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
	t.Run("first ten concurrent subscribers get all messages, send acks", func(t *testing.T) {
		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				sub, err := broker.NewSubscriber(topic)
				if !assert.NoError(t, err) {
					return
				}
				defer sub.Close()

				i := 0
			items:
				for {
					select {
					case msg := <-sub.Messages():
						if !assert.Equal(t, fmt.Sprintf("msg%d", i), string(msg.Data())) {
							return
						}
						err = msg.Acker()()
						if !assert.NoError(t, err) {
							return
						}

						i++
					case <-time.After(5 * time.Second):
						break items
					}
				}
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
	})

	if true {
		return
	}

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
