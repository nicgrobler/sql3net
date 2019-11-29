package main

import (
	"context"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	IDLE_CONNECTION_TIMEOUT int = 2
)

// HttpServer is the basic abstraction used for handling http requests
type HttpServer struct {
	slug     string
	address  string
	Done     chan error
	router   *http.ServeMux
	listener *http.Server
}

// NewHTTPServer takes a valid address - can be of form IP:Port, or :Port - and returns a server
func NewHTTPServer(description, address string) *HttpServer {
	s := &HttpServer{slug: description, address: address, Done: make(chan error), router: http.NewServeMux()}
	s.setListener(&http.Server{Addr: address, Handler: s.router, IdleTimeout: time.Duration(IDLE_CONNECTION_TIMEOUT)})
	return s
}

func (s *HttpServer) setListener(l *http.Server) {
	s.listener = l
}

// RegisterHandler allows caller to set routing and handler functions as needed
func (s *HttpServer) RegisterHandler(path string, handlerfn func(http.ResponseWriter, *http.Request)) {
	s.router.HandleFunc(path, handlerfn)
}

// StartListener starts the server's listener with a context, allowing for later graceful shutdown.
// the supplied timeout is the amount of time that is allowed before the server forcefully
// closes any remaining conections. Once done close Done channel
// note: this is a blocking call
func (s *HttpServer) StartListener(ctx context.Context, timeout time.Duration) {

	go func() {
		if err := s.listener.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http listen:%+s", err)
		}
	}()

	log.Info(s.slug + " on: " + s.address)

	<-ctx.Done()

	log.Info(s.slug + " stopping")

	ctxShutDown, cancel := context.WithTimeout(context.Background(), timeout)
	defer func() {
		cancel()
	}()

	if err := s.listener.Shutdown(ctxShutDown); err != nil {
		log.Warnf(s.slug+" graceful shutdown failed:%+s", err)
		if e := s.listener.Close(); e != nil {
			log.Fatalf(s.slug+" forced shutdown failed:%+s", err)
		} else {
			log.Info("forced shutdown ok")
		}
	}

	log.Info(s.slug + " stopped")

	// let parent know that we are done
	close(s.Done)
}
