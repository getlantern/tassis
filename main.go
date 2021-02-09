package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/golog"
	"github.com/getlantern/keyman"

	"github.com/getlantern/tassis/broker/redisbroker"
	"github.com/getlantern/tassis/db/redisdb"
	"github.com/getlantern/tassis/service"
	"github.com/getlantern/tassis/testsupport"
	"github.com/getlantern/tassis/web"
)

var (
	httpPort            = os.Getenv("PORT") // passed by Herok
	pprofAddr           = os.Getenv("PPROF_ADDR")
	redisURL            = os.Getenv("REDIS_URL")
	redisPoolSize       = os.Getenv("REDIS_POOL_SIZE")
	redisCA             = os.Getenv("REDIS_CA")
	redisClientKeyFile  = flag.String("pkfile", "", "Redis private key file")
	redisClientCertFile = flag.String("certfile", "", "Redis certificate file")
	checkKeysInterval   = flag.Duration("checkprekeys", 5*time.Minute, "how frequently to check if device is low on prekeys")
	lowPreKeysLimit     = flag.Int("lowprekeyslimit", 10, "what number of prekeys ")
	webTimeout          = flag.Duration("webtimeout", 60*time.Second, "timeout for web requests")

	log = golog.LoggerFor("tassis")
)

var (
	redisURLRegExp = regexp.MustCompile(`^redis(s?)://:(.+)?@([^\s]+)$`)
)

func parseRedisURL(redisURL string) (useHTTPS bool, password string, redisAddr string, err error) {
	matches := redisURLRegExp.FindStringSubmatch(redisURL)
	if len(matches) < 4 {
		return false, "", "", fmt.Errorf("should match %v", redisURLRegExp.String())
	}
	return matches[1] == "s", matches[2], matches[3], nil
}

func main() {
	flag.Parse()

	if pprofAddr != "" {
		go func() {
			log.Error(http.ListenAndServe(pprofAddr, nil))
		}()
	}

	if httpPort == "" {
		log.Fatal("Missing PORT environment variable")
	}

	log.Debugf("Using web timeout of %v", *webTimeout)

	useTLS, redisPassword, redisAddr, err := parseRedisURL(redisURL)
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}

	var tlsConfig *tls.Config

	if !useTLS {
		log.Debug("WARNING: connecting to Redis without TLS")
	} else {
		log.Debug("Connecting to Redis with TLS")
		if redisCA == "" {
			log.Fatal("Please specify a REDIS_CA")
		}
		// if _, err := os.Stat(*redisCAFile); os.IsNotExist(err) {
		// 	log.Fatal("Cannot find certificate authority file")
		// }
		// if *redisClientKeyFile == "" {
		// 	log.Fatal("Please set a client private key file")
		// }
		// if _, err := os.Stat(*redisClientKeyFile); os.IsNotExist(err) {
		// 	log.Fatal("Cannot find client private key file")
		// }
		// if *redisClientCertFile == "" {
		// 	log.Fatal("Please set a client certificate file")
		// }
		// if _, err := os.Stat(*redisClientCertFile); os.IsNotExist(err) {
		// 	log.Fatal("Cannot find client certificate file")
		// }

		// redisClientCert, err := tls.LoadX509KeyPair(*redisClientCertFile, *redisClientKeyFile)
		// if err != nil {
		// 	log.Fatalf("Failed to load client certificate: %v", err)
		// }
		redisCACert, err := keyman.LoadCertificateFromPEMBytes([]byte(redisCA))
		if err != nil {
			log.Fatalf("Failed to load CA cert: %v", err)
		}

		tlsConfig = &tls.Config{
			RootCAs: redisCACert.PoolContainingCert(),
			// Certificates:       []tls.Certificate{redisClientCert},
			ClientSessionCache: tls.NewLRUClientSessionCache(100),
		}
	}

	poolSize, err := strconv.Atoi(redisPoolSize)
	if err != nil {
		log.Debug("Defaulting redis pool size to 100")
		poolSize = 100
	}

	opTimeout := *webTimeout - 500*time.Millisecond
	redisOpts := &redis.Options{
		Addr:         redisAddr,
		Password:     redisPassword,
		PoolSize:     poolSize,
		PoolTimeout:  opTimeout,
		ReadTimeout:  opTimeout,
		WriteTimeout: opTimeout,
		IdleTimeout:  opTimeout,
		DialTimeout:  opTimeout,
		TLSConfig:    tlsConfig,
	}

	client := redis.NewClient(redisOpts)

	b := redisbroker.New(client)
	d, err := redisdb.New(client)
	if err != nil {
		log.Fatalf("unable to start redisdb: %v", err)
	}

	srvc, err := service.New(&service.Opts{
		DB:                   d,
		Broker:               b,
		CheckPreKeysInterval: testsupport.CheckPreKeysInterval,
		LowPreKeysLimit:      testsupport.LowPreKeysLimit,
		NumPreKeysToRequest:  testsupport.NumPreKeysToRequest,
	})

	h := web.NewHandler(srvc)

	srv := &http.Server{
		Addr:         ":" + httpPort,
		Handler:      h,
		ReadTimeout:  *webTimeout,
		WriteTimeout: *webTimeout,
	}
	log.Fatal(srv.ListenAndServe())
}

func toDuration(duration string, onErr time.Duration) time.Duration {
	if parsed, err := time.ParseDuration(duration); err != nil {
		return onErr
	} else {
		return parsed
	}
}

func toFloat(str string, onErr float64) float64 {
	if f, err := strconv.ParseFloat(str, 64); err != nil {
		return onErr
	} else {
		return f
	}
}
