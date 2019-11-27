package main

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNetHandlerQuery(t *testing.T) {
	fakeDB, mock, err := sqlmock.New()
	assert.NoError(t, err)

	// verify that the nethandler routes the incoming query correctly
	fc := GetFakeConn("0.0.0.0")
	c := &QConn{}
	c.Conn = fc
	pool := getEmptyPool()
	c.pool = &pool
	pool.addToPool(c)

	// load the query into the connection
	c.Write([]byte("select * from apples"))

	fs := &fileStore{lock: &sync.Mutex{}}
	fs.store = make(map[string]*q3file)
	fs.store["mysterious.db"] = &q3file{
		path: "/fakefile",
		db:   fakeDB,
		lock: &sync.RWMutex{},
	}

	// create mocked db return
	mock.ExpectQuery("select \\* from apples").WillReturnRows(sqlmock.NewRows([]string{"id", "age"}).AddRow("1", "899"))

	fs.netHandler(c)

	// if routing worked as it should, this should be going to QUERY
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestNetHandlerExec(t *testing.T) {
	fakeDB, mock, err := sqlmock.New()
	assert.NoError(t, err)

	// verify that the nethandler routes the incoming query correctly
	fc := GetFakeConn("0.0.0.0")
	c := &QConn{}
	c.Conn = fc
	pool := getEmptyPool()
	c.pool = &pool
	pool.addToPool(c)

	// load the query into the connection
	c.Write([]byte("insert into apples(id) value(1)"))

	fs := &fileStore{lock: &sync.Mutex{}}
	fs.store = make(map[string]*q3file)
	fs.store["mysterious.db"] = &q3file{
		path: "/fakefile",
		db:   fakeDB,
		lock: &sync.RWMutex{},
	}

	// create mocked db return
	mock.ExpectExec("insert into apples\\(id\\) value\\(1\\)").WillReturnResult(sqlmock.NewResult(0, 0))

	fs.netHandler(c)

	// if routing worked as it should, this should be going to QUERY
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestNetHandlerErrors(t *testing.T) {
	// verify that the nethandler routes the incoming query correctly
	fc := GetFakeConn("0.0.0.0")
	c := &QConn{}
	c.Conn = fc
	c.MaxReadBuffer = 20
	pool := getEmptyPool()
	c.pool = &pool
	pool.addToPool(c)

	// load empty query into the connection
	c.Write([]byte("\n"))

	fs := &fileStore{lock: &sync.Mutex{}}
	fs.store = make(map[string]*q3file)
	fs.store["mysterious.db"] = &q3file{
		path: "/fakefile",
		lock: &sync.RWMutex{},
	}

	fs.netHandler(c)

	b := make([]byte, 14)
	c.Read(b)

	assert.Equal(t, "\nempty string\n", string(b))

}

func TestHTTPHandlerQuery(t *testing.T) {
	fakeDB, mock, err := sqlmock.New()
	assert.NoError(t, err)

	// verify that the nethandler routes the incoming query correctly
	fc := GetFakeConn("0.0.0.0")
	c := &QConn{}
	c.Conn = fc
	pool := getEmptyPool()
	c.pool = &pool
	pool.addToPool(c)

	// load the query into the connection
	c.Write([]byte("select * from apples"))

	fs := &fileStore{lock: &sync.Mutex{}}
	fs.store = make(map[string]*q3file)
	fs.store["mysterious.db"] = &q3file{
		path: "/fakefile",
		db:   fakeDB,
		lock: &sync.RWMutex{},
	}

	// create mocked db return
	mock.ExpectQuery("select \\* from apples").WillReturnRows(sqlmock.NewRows([]string{"id", "age"}).AddRow("1", "899"))

	fakeRequest := &http.Request{Body: c.Conn}

	fs.httpReadHandler(fakeWriter{}, fakeRequest)

	// if routing worked as it should, this should be going to QUERY
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestHTTPHandlerExec(t *testing.T) {
	fakeDB, mock, err := sqlmock.New()
	assert.NoError(t, err)

	// verify that the nethandler routes the incoming query correctly
	fc := GetFakeConn("0.0.0.0")
	c := &QConn{}
	c.Conn = fc
	pool := getEmptyPool()
	c.pool = &pool
	pool.addToPool(c)

	// load the query into the connection
	c.Write([]byte("insert into apples(id) value(1)"))

	fs := &fileStore{lock: &sync.Mutex{}}
	fs.store = make(map[string]*q3file)
	fs.store["mysterious.db"] = &q3file{
		path: "/fakefile",
		db:   fakeDB,
		lock: &sync.RWMutex{},
	}

	// create mocked db return
	mock.ExpectExec("insert into apples\\(id\\) value\\(1\\)").WillReturnResult(sqlmock.NewResult(0, 0))

	fakeRequest := &http.Request{Body: c.Conn}

	fs.httpWriteHandler(fakeWriter{}, fakeRequest)

	// if routing worked as it should, this should be going to QUERY
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

type fakeWriter struct{}

func (f fakeWriter) Header() http.Header        { return http.Header{} }
func (f fakeWriter) Write([]byte) (int, error)  { return 0, nil }
func (f fakeWriter) WriteHeader(statusCode int) { return }
