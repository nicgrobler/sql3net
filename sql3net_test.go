package main

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DATA-DOG/go-sqlmock"
)

func clearEnv() {
	os.Setenv("HOST_INTERFACE", "")
	os.Setenv("NET_PORT", "")
	os.Setenv("HTTP_PORT", "")
	os.Setenv("IDLE_TIMEOUT", "")
	os.Setenv("LOG_LEVEL", "")

}

func TestGetConfig(t *testing.T) {
	// clear env settings
	clearEnv()
	c := getConfig()
	// confirm defaults get set
	assert.Equal(t, "0.0.0.0", c.HostInterface)
	assert.Equal(t, PORT, c.NetPort)
	assert.Equal(t, HTTP_PORT, c.HTTPPort)
	assert.Equal(t, CONNECTION_CLOSE_TIME_LIMIT, c.IdleTimeout)
	assert.Equal(t, "info", c.LoggingLevel)

	// now confirm that overrides work
	os.Setenv("HOST_INTERFACE", "1.2.3.4")
	os.Setenv("NET_PORT", "1234")

	c = getConfig()
	assert.Equal(t, "1.2.3.4", c.HostInterface)
	assert.Equal(t, "1234", c.NetPort)
	assert.Equal(t, HTTP_PORT, c.HTTPPort)
	assert.Equal(t, CONNECTION_CLOSE_TIME_LIMIT, c.IdleTimeout)
	assert.Equal(t, "info", c.LoggingLevel)

}

func TestGetIDAndQuery(t *testing.T) {
	data := []byte("a1b2c3;;select * from bla;")
	id, query := getIDAndQuery(data)
	assert.Equal(t, "a1b2c3", id, "should be the same")
	assert.Equal(t, "select * from bla;", string(query), "should be the same")

	idTooLong := []byte("a1b2c3-ezrwzureouzqwr-23756gfidgkhvcbk;;select * from bla;")
	id, query = getIDAndQuery(idTooLong)
	assert.Equal(t, "", id, "should be the same")
	assert.Equal(t, "a1b2c3-ezrwzureouzqwr-23756gfidgkhvcbk;;select * from bla;", string(query), "should be the same")

	data = []byte(";;select * from bla;")
	id, query = getIDAndQuery(data)
	assert.Equal(t, "", id, "should be the same")
	assert.Equal(t, "select * from bla;", string(query), "should be the same")

}

func TestExctractIdentifier(t *testing.T) {
	data := []byte("a1b2c3;;select * from bla;")
	id, queryOffset := exctractIdentifier(data)
	assert.Equal(t, "a1b2c3", id, "the id should match this string")
	assert.Equal(t, "select * from bla;", string(data[queryOffset:]), "the query should match this string")

	idTooLong := []byte("a1b2c3-ezrwzureouzqwr-23756gfidgkhvcbk;;select * from bla;")
	id, queryOffset = exctractIdentifier(idTooLong)
	assert.Equal(t, "", id, "the id should match this string")
	assert.Equal(t, "a1b2c3-ezrwzureouzqwr-23756gfidgkhvcbk;;select * from bla;", string(idTooLong[queryOffset:]), "the query should match this string")

	data = []byte(";;select * from bla;")
	id, queryOffset = exctractIdentifier(data)
	assert.Equal(t, "", id, "the id should match this string")
	assert.Equal(t, "select * from bla;", string(data[queryOffset:]), "the query should match this string")
}

func TestQueryOffset(t *testing.T) {
	data := []byte("a1b2c3;;select * from bla;")
	offset, found := queryOffset(data)
	assert.Equal(t, 8, offset, "the query should start from here")
	assert.True(t, found, "should be true as found")

	data = []byte(";;select * from bla;")
	offset, found = queryOffset(data)
	assert.Equal(t, 2, offset, "the query should start from here")
	assert.False(t, found, "first characters are ;;, so should be false, as no identifier")

	data = []byte("select * from bla;")
	offset, found = queryOffset(data)
	assert.Equal(t, 0, offset, "the query should start from here")
	assert.False(t, found, ";; not within first 32 chars, so should be false")

}

func TestWriteError(t *testing.T) {
	b := &bytes.Buffer{}
	expectedError := errors.New("oh dear - this is a BAD error. BAD.")
	writeError(b, expectedError)
	assert.Equal(t, expectedError.Error()+"\n", string(b.Bytes()), "these should be exactly the same")
}

func TestPathValid(t *testing.T) {
	assert.False(t, pathIsValid(""))
	assert.False(t, pathIsValid("  "))
	assert.False(t, pathIsValid("_ d_"))
	assert.True(t, pathIsValid("0.0.0.0"))
	assert.True(t, pathIsValid("::1"))
	assert.True(t, pathIsValid(":"))
	assert.False(t, pathIsValid("\\"))
	assert.False(t, pathIsValid("'"))
	assert.False(t, pathIsValid("`"))
	assert.False(t, pathIsValid("'"))
	assert.False(t, pathIsValid(";"))
	assert.False(t, pathIsValid("*"))
}

func TestQ3FileInit(t *testing.T) {
	// invalid
	fs := &fileStore{lock: &sync.Mutex{}}
	fs.store = make(map[string]*q3file)
	fs.store["0.0.0.0.db"] = &q3file{
		path: "/fakefile",
		lock: &sync.RWMutex{},
	}

	// valid path
	f, err := fs.getFile("0.0.0.0.db")
	assert.NotNil(t, f, "should not be nil")
	assert.Nil(t, err, "should be nil")

	// invalid path
	f, err = fs.getFile("")
	assert.Nil(t, f, "should be nil")
	assert.Equal(t, "invalid filename supplied: ''", err.Error())

}

func TestGetIPWithoutPort(t *testing.T) {
	assert.Equal(t, "1.2.3.4", getIPWithoutPort("1.2.3.4"), "should be the same")
	assert.Equal(t, "1.2.3.4", getIPWithoutPort("1.2.3.4:80"), "should be the same")
	assert.Equal(t, "1.2.3", getIPWithoutPort("1.2.3:4"), "should be the same")
	assert.Equal(t, "1::2:3", getIPWithoutPort("1::2:3:80"), "should be the same")
	assert.Equal(t, "[::1]", getIPWithoutPort("[::1]:80"), "should be the same")
	assert.Equal(t, "::12", getIPWithoutPort("::12"), "should be the same")
	assert.Equal(t, "::12", getIPWithoutPort("::12:8080"), "should be the same")
}

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
	c.Write([]byte("a1f4-5ghghg;;select * from apples"))

	fs := &fileStore{lock: &sync.Mutex{}}
	fs.store = make(map[string]*q3file)
	fs.store["a1f4-5ghghg.db"] = &q3file{
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
	fs.store["5.6.7.8.db"] = &q3file{
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
	fs.store["5.6.7.8.db"] = &q3file{
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
	fs.store["5.6.7.8.db"] = &q3file{
		path: "/fakefile",
		db:   fakeDB,
		lock: &sync.RWMutex{},
	}

	// create mocked db return
	mock.ExpectQuery("select \\* from apples").WillReturnRows(sqlmock.NewRows([]string{"id", "age"}).AddRow("1", "899"))

	fakeRequest := &http.Request{Body: c.Conn, RemoteAddr: c.Conn.RemoteAddr().String()}

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
	fs.store["5.6.7.8.db"] = &q3file{
		path: "/fakefile",
		db:   fakeDB,
		lock: &sync.RWMutex{},
	}

	// create mocked db return
	mock.ExpectExec("insert into apples\\(id\\) value\\(1\\)").WillReturnResult(sqlmock.NewResult(0, 0))

	fakeRequest := &http.Request{Body: c.Conn, RemoteAddr: c.Conn.RemoteAddr().String()}

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
