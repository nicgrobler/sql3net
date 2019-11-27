package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestStartHTTPListener(t *testing.T) {
	log.SetLevel(log.WarnLevel)
	// grab ctx to pass onto server(s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := false
	acceptedConnection := &b

	// initialise net listener
	httpServer := NewHTTPServer("http listener", "127.0.0.1:1234")
	httpServer.RegisterHandler("/nonesenseDoNothingHandler", func(http.ResponseWriter, *http.Request) { *acceptedConnection = true })
	go httpServer.StartListener(ctx, time.Duration(50*time.Millisecond))
	// handler not called yet
	assert.Equal(t, false, *acceptedConnection, "These should be equal")
	var closed bool
	go func(c *bool) {
		<-httpServer.Done
		*c = true
	}(&closed)

	// call, and confirm that handler fired
	http.Get("http://127.0.0.1:1234/nonesenseDoNothingHandler")
	// give it some time to actually run
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, true, *acceptedConnection, "These should be equal")
	// check that server still listening
	assert.Equal(t, false, closed, "should be equal")
	// kill server
	cancel()
	// give it some time to actually run
	time.Sleep(100 * time.Millisecond)
	// check that server shutdown
	assert.Equal(t, true, closed, "should be equal")

}
