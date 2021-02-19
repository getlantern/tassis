package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/getlantern/golog"

	"github.com/getlantern/tassis/broker/redisbroker"
	"github.com/getlantern/tassis/db/redisdb"
	"github.com/getlantern/tassis/service/serviceimpl"
	"github.com/getlantern/tassis/testsupport"
	"github.com/getlantern/tassis/web"
)

var (
	httpPort           = os.Getenv("PORT") // passed by Herok
	pprofAddr          = os.Getenv("PPROF_ADDR")
	redisURL           = os.Getenv("REDIS_URL")
	redisPoolSize      = os.Getenv("REDIS_POOL_SIZE")
	redisCAPEM         = os.Getenv("REDIS_CA_CERT")
	redisClientCertPEM = os.Getenv("REDIS_CLIENT_CERT")
	redisClientKeyPEM  = os.Getenv("REDIS_CLIENT_KEY")
	checkKeysInterval  = flag.Duration("checkprekeys", 5*time.Minute, "how frequently to check if device is low on prekeys")
	lowPreKeysLimit    = flag.Int("lowprekeyslimit", 10, "what number of prekeys ")
	webTimeout         = flag.Duration("webtimeout", 60*time.Second, "timeout for web requests")

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

	log.Debugf("Connecting to redis at %v", redisAddr)

	var tlsConfig *tls.Config
	if !useTLS {
		log.Debug("WARNING: connecting to Redis without TLS")
	} else {
		log.Debug("Connecting to Redis with TLS")
		if redisCAPEM == "" {
			log.Fatal("Please specify a REDIS_CA_CERT")
		}
		if redisClientCertPEM == "" {
			log.Fatal("Please specify a REDIS_CLIENT_CERT")
		}
		if redisClientKeyPEM == "" {
			log.Fatal("Please specify a REDIS_CLIENT_KEY")
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cleanPEMNewLines(redisCAPEM)) {
			log.Fatal("Unable to find any certs in REDIS_CA_CERT")
		}
		redisClientCert, err := tls.X509KeyPair(cleanPEMNewLines(redisClientCertPEM), cleanPEMNewLines(redisClientKeyPEM))
		if err != nil {
			log.Fatalf("Failed to load Redis Client cert and key: %v", err)
		}

		tlsConfig = &tls.Config{
			RootCAs:            pool,
			Certificates:       []tls.Certificate{redisClientCert},
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

	srvc, err := serviceimpl.New(&serviceimpl.Opts{
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

func cleanPEMNewLines(pem string) []byte {
	return []byte(strings.Replace(pem, "\\n", "\n", -1))
}
