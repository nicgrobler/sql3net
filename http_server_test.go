package main

import (
	"bytes"
	"net/http"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func TestHTTPRouting(t *testing.T) {
	// create our fake data store
	fakeDB, mock, err := sqlmock.New()
	assert.NoError(t, err)
	// create mocked db return
	mock.ExpectQuery("select \\* from apples").WillReturnRows(sqlmock.NewRows([]string{"id", "age"}).AddRow("1", "899"))

	fs := &fileStore{lock: &sync.Mutex{}}
	fs.store = make(map[string]*q3file)
	fs.store["1.2.3.4.db"] = &q3file{
		path: "/fakefile",
		db:   fakeDB,
		lock: &sync.RWMutex{},
	}

	// as the handlers are tested elsewhere, here we are testing the routing of the incoming connections
	// to ensure that the correct handlers are always called
	// initialise http listener
	httpServer := NewHTTPServer("http listener", "localhost")
	httpServer.RegisterHandler("/read", fs.httpReadHandler)
	httpServer.RegisterHandler("/write", fs.httpWriteHandler)

	// as we are not testing the network, and want to be able reproduce this without relying on OS to supply ports for each test (and tearing them down afterwards)
	// we invoke the MUX as would happen on connect
	body := bytes.NewBuffer([]byte("select * from apples"))
	r, err := http.NewRequest("POST", "http://localhost/read", body)
	r.RemoteAddr = "1.2.3.4:80"
	assert.Nil(t, err, "should be nil")

	f := fakeHttpWriter{b: &bytes.Buffer{}}
	httpServer.router.ServeHTTP(f, r)

	// if routing worked as it should, this should be going to QUERY
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}

}

type fakeHttpWriter struct {
	b *bytes.Buffer
}

func (f fakeHttpWriter) Header() http.Header { return http.Header{} }
func (f fakeHttpWriter) WriteHeader(int)     { return }
func (f fakeHttpWriter) Write(d []byte) (int, error) {
	return f.b.Write(d)
}
