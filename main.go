package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

const (
	DELIM                                     = "|"
	LINE_END                                  = "\n"
	PORT                                      = "3030"
	HTTP_PORT                                 = "9090"
	DIRECTORY_NAME                            = "filestore"
	NUMBER_OF_LISTENERS         int           = 2
	CONNECTION_CLOSE_TIME_LIMIT time.Duration = 1
)

type Config struct {
	HostInterface string
	NetPort       string
	HTTPPort      string
	IdleTimeout   time.Duration
	LoggingLevel  string
}

type fileStore struct {
	store map[string]*q3file
	lock  *sync.Mutex
}

type q3file struct {
	path  string
	db    *sql.DB
	lock  *sync.RWMutex
	valid bool
}

func getConfig() *Config {
	c := &Config{}
	c.HostInterface = "0.0.0.0"
	c.NetPort = PORT
	c.HTTPPort = HTTP_PORT
	c.IdleTimeout = CONNECTION_CLOSE_TIME_LIMIT
	c.LoggingLevel = "info"
	c.updateConfig()

	return c
}

func (c *Config) updateConfig() {
	if s := os.Getenv("HOST_INTERFACE"); s != "" {
		c.HostInterface = s
	}
	if s := os.Getenv("NET_PORT"); s != "" {
		c.NetPort = s
	}
	if s := os.Getenv("HTTP_PORT"); s != "" {
		c.HTTPPort = s
	}
	if s := os.Getenv("IDLE_TIMEOUT"); s != "" {
		i, err := strconv.Atoi(s)
		if err == nil {
			c.IdleTimeout = time.Duration(i) * time.Second
		}
	}
	if s := os.Getenv("LOG_LEVEL"); s != "" {
		c.LoggingLevel = s
	}
}

func (f *fileStore) getFile(fileName string) (*q3file, error) {
	// verify that this path is valid
	if !pathIsValid(fileName) {
		return nil, errors.New("invalid filename supplied")
	}
	// if file not already in store, create it, then return it
	f.lock.Lock()
	defer f.lock.Unlock()
	file, found := f.store[fileName]
	if !found {
		newFile := new(q3file)
		newFile.path = DIRECTORY_NAME + "/" + fileName
		newFile.lock = &sync.RWMutex{}
		newFile.init()
		f.store[fileName] = newFile
		file = newFile
	}
	return file, nil
}

func (f *q3file) getPath() string {
	return f.path
}

func pathIsValid(path string) bool {
	// validate the supplied path
	if strings.TrimSpace(path) == "" {
		return false
	}
	// we have a dsn that MIGHT be valid, so need to parse it - if it fails here, it is likely to be invalid
	_, err := neturl.Parse(path)
	if err != nil {
		return false
	}
	return true
}

func (f *q3file) init() {
	srcDb, err := sql.Open("sqlite3", f.getPath())
	if err != nil {
		log.Fatal(err)
	}
	f.db = srcDb
	f.valid = true
}

func (f *q3file) close() {
	f.db.Close()
}

func (f *q3file) exec(insert string) (sql.Result, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.db.Exec(insert)
}

func (f *q3file) query(query string) (*sql.Rows, error) {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return f.db.Query(query)
}

func writeError(conn net.Conn, err error) {
	conn.Write([]byte(err.Error()))
	conn.Write([]byte(LINE_END))
}

func writeHTTPError(w http.ResponseWriter, err error) {
	w.Write([]byte(err.Error()))
	w.Write([]byte(LINE_END))
}

func (f *q3file) rowsPrinter(conn net.Conn, rows *sql.Rows) {
	// column names
	columns, err := rows.Columns()
	if err != nil {
		writeError(conn, err)
	}

	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	for rows.Next() {
		err = rows.Scan(scanArgs...)
		if err != nil {
			writeError(conn, err)
		}
		// now print the row, column-wise
		for i := range values {
			conn.Write(values[i])
			if i < len(values)-1 {
				conn.Write([]byte(DELIM))
			}
		}
		conn.Write([]byte(LINE_END))
	}
	if err = rows.Err(); err != nil {
		writeError(conn, err)
	}

}

func (f *q3file) rowsHTTPPrinter(w http.ResponseWriter, rows *sql.Rows) {
	// column names
	columns, err := rows.Columns()
	if err != nil {
		writeHTTPError(w, err)
	}

	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	for rows.Next() {
		err = rows.Scan(scanArgs...)
		if err != nil {
			writeHTTPError(w, err)
		}
		// now print the row, column-wise
		for i := range values {
			w.Write(values[i])
			if i < len(values)-1 {
				w.Write([]byte(DELIM))
			}
		}
		w.Write([]byte(LINE_END))
	}
	if err = rows.Err(); err != nil {
		writeHTTPError(w, err)
	}

}

func (f *fileStore) netHandler(connection *QConn) {
	defer connection.Close()
	buf := bytes.Buffer{}
	// check input stream
	copied, err := io.Copy(&buf, connection.Conn)
	if err != nil {
		writeError(connection.Conn, err)
		return
	}
	if copied < 1 {
		writeError(connection.Conn, errors.New("empty string"))
		return
	}
	if len(strings.TrimSpace(string(buf.Bytes()))) == 0 {
		writeError(connection.Conn, errors.New("empty string"))
		return
	}
	q3f, err := f.getFile(connection.GetDBName())
	if err != nil {
		writeError(connection.Conn, err)
		return
	}
	// route depending on Query or Exec
	s := strings.Split(strings.ToLower(string(buf.Bytes())), " ")
	if s[0] == "select" {
		rows, err := q3f.query(string(buf.Bytes()))
		if err != nil {
			writeError(connection.Conn, err)
		} else {
			q3f.rowsPrinter(connection.Conn, rows)
		}
	} else {
		_, err := q3f.exec(string(buf.Bytes()))
		if err != nil {
			writeError(connection.Conn, err)
		} else {
			// TODO - perhaps return rows affected.
		}
	}
}

func (f *fileStore) httpReadHandler(w http.ResponseWriter, r *http.Request) {
	log.Debugf("accepted connection from: %v", r.RemoteAddr)
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	if len(b) < 1 {
		writeHTTPError(w, errors.New("empty string"))
		return
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		writeHTTPError(w, errors.New("empty string"))
		return
	}
	q3f, err := f.getFile(getHTTPDBName(r))
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	rows, err := q3f.query(string(b))
	if err != nil {
		writeHTTPError(w, err)
	} else {
		q3f.rowsHTTPPrinter(w, rows)
	}

}

func (f *fileStore) httpWriteHandler(w http.ResponseWriter, r *http.Request) {
	log.Debugf("accepted connection from: %v", r.RemoteAddr)
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	if len(b) < 1 {
		writeHTTPError(w, errors.New("empty string"))
		return
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		writeHTTPError(w, errors.New("empty string"))
		return
	}
	q3f, err := f.getFile(getHTTPDBName(r))
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	_, err = q3f.exec(string(b))
	if err != nil {
		writeHTTPError(w, err)
	} else {
		// TODO - perhaps return rows affected.
	}
}

func getHTTPDBName(r *http.Request) string {
	address := r.RemoteAddr
	if address != "" {
		bits := strings.Split(address, ":")
		return bits[0] + ".db"
	}
	return ""
}

func getLocalAddress() string {
	// this doesn't actually need to connect for it to work
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		// TODO - handle error
	}
	ipPort := conn.LocalAddr().String()
	if strings.Contains(ipPort, ":") {
		return strings.Split(ipPort, ":")[0]
	}
	return ipPort

}

func createPath() {
	if _, err := os.Stat(DIRECTORY_NAME); os.IsNotExist(err) {
		os.Mkdir(DIRECTORY_NAME, 755)
	}
}

// signalContext listens for os signals, and when received, calls cancel on returned context.
func signalContext() context.Context {

	// listen for any and all signals
	c := make(chan os.Signal, 1)
	signal.Notify(c)

	// set context so we can cancel the listner(s)
	ctx, cancel := context.WithCancel(context.Background())

	// prepare to cancel context on receipt of os signal
	go func() {
		oscall := <-c
		log.Infof("received signal: %+v", oscall)
		cancel()
	}()

	return ctx

}

func main() {
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	log.SetLevel(log.InfoLevel)

	c := getConfig()

	// set logging
	level := c.LoggingLevel
	var logLevel logrus.Level
	if err := logLevel.UnmarshalText([]byte(level)); err != nil {
		log.Fatal(err)
	}
	log.Info("logging set to: ", logLevel.String())
	log.SetLevel(logLevel)

	// create the main data directory if not already there
	createPath()

	// init store
	f := &fileStore{store: make(map[string]*q3file), lock: &sync.Mutex{}}

	// grab ctx to pass onto server(s)
	ctx := signalContext()

	// initialise http listener
	httpServer := NewHTTPServer("http listener", c.HostInterface+":"+c.HTTPPort)
	httpServer.RegisterHandler("/read", f.httpReadHandler)
	httpServer.RegisterHandler("/write", f.httpWriteHandler)

	// initialise net listener
	netServer := NewNETServer(ctx, "net listener", c.HostInterface+":"+c.NetPort)
	netServer.RegisterHandler(f.netHandler)

	// run http server's listener
	go httpServer.StartListener(ctx, time.Duration(CONNECTION_CLOSE_TIME_LIMIT*time.Second))

	// run net server's listener
	go netServer.StartListener(ctx, time.Duration(CONNECTION_CLOSE_TIME_LIMIT*time.Second))

	// wait for all to complete
	<-netServer.Done
	<-httpServer.Done

	log.Info("all stopped")

}
