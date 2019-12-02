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
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

const (
	DELIM                                     = "|"
	SEPERATOR_LENGTH                          = 2
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

func (f *fileStore) getFile(identifier string) (*q3file, error) {

	// verify that this path is valid
	if !pathIsValid(identifier) {
		return nil, errors.New("invalid filename supplied: '" + identifier + "'")
	}
	// filename is made up of supplied identifier, and ".db"
	fileName := identifier + ".db"
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
	/*
		validate the supplied path - the following cause us to return FALSE as
		they're a bad idea for filepaths on Unix:

		forward slash (/)
		backslash (\)
		NULL (\0)
		tick (`)
		starts with a dash (-)
		star (*)
		pipes (|)
		semicolon (;)
		quotations (" or ')

	*/
	if strings.TrimSpace(path) == "" {
		return false
	}
	if strings.Contains(path, "\\") {
		return false
	}
	if strings.Contains(path, " ") {
		return false
	}
	if strings.Contains(path, "`") {
		return false
	}
	if strings.HasPrefix(path, "-") {
		return false
	}
	if strings.Contains(path, "*") {
		return false
	}
	if strings.Contains(path, "|") {
		return false
	}
	if strings.Contains(path, ";") {
		return false
	}
	if strings.Contains(path, "\"") {
		return false
	}
	if strings.Contains(path, "'") {
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

func writeError(conn io.Writer, err error) {
	conn.Write([]byte(err.Error()))
	conn.Write([]byte(LINE_END))
}

func (f *q3file) rowsPrinter(w io.Writer, rows *sql.Rows) {
	// column names
	columns, err := rows.Columns()
	if err != nil {
		writeError(w, err)
	}
	defer rows.Close()

	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	for rows.Next() {
		err = rows.Scan(scanArgs...)
		if err != nil {
			writeError(w, err)
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
		writeError(w, err)
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

	id, query := getIDAndQuery(buf.Bytes())
	if id == "" {
		id = connection.GetDBName()
	}
	q3f, err := f.getFile(id)
	if err != nil {
		writeError(connection.Conn, err)
		return
	}
	// route depending on Query or Exec
	s := strings.Split(strings.ToLower(string(query)), " ")
	if s[0] == "select" {
		rows, err := q3f.query(string(query))
		if err != nil {
			writeError(connection.Conn, err)
		} else {
			q3f.rowsPrinter(connection.Conn, rows)
		}
	} else {
		_, err := q3f.exec(string(query))
		if err != nil {
			writeError(connection.Conn, err)
		} else {
			// TODO - perhaps return rows affected.
		}
	}
}

func (f *fileStore) httpReadHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	log.Debugf("accepted connection from: %v", r.RemoteAddr)
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(b) < 1 {
		writeError(w, errors.New("empty string"))
		return
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		writeError(w, errors.New("empty string"))
		return
	}
	id, query := getIDAndQuery(b)
	if id == "" {
		id = getHTTPDBName(r)
	}
	q3f, err := f.getFile(id)
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := q3f.query(string(query))
	if err != nil {
		writeError(w, err)
	} else {
		q3f.rowsPrinter(w, rows)
	}

}

func (f *fileStore) httpWriteHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	log.Debugf("accepted connection from: %v", r.RemoteAddr)
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(b) < 1 {
		writeError(w, errors.New("empty string"))
		return
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		writeError(w, errors.New("empty string"))
		return
	}
	id, query := getIDAndQuery(b)
	if id == "" {
		id = getHTTPDBName(r)
	}
	q3f, err := f.getFile(id)
	if err != nil {
		writeError(w, err)
		return
	}
	_, err = q3f.exec(string(query))
	if err != nil {
		writeError(w, err)
	} else {
		// TODO - perhaps return rows affected.
	}
}

func getIPWithoutPort(addr string) string {
	if addr != "" {
		// find last instance of ':', and split here
		index := strings.LastIndex(addr, ":")
		if index > 0 {
			// on the offchance that we have an address such as '::1'
			// we need to check if preceding index is also ':'
			if addr[index-1] != ':' {
				address := addr[0:index]
				return address
			}
		}
		// no ':' found, so no port substring
		return addr
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

func getIDAndQuery(data []byte) (string, []byte) {
	identifier, queryOffset := exctractIdentifier(data)
	query := data[queryOffset:]
	return identifier, query
}

func exctractIdentifier(data []byte) (string, int) {
	identifier := ""
	queryStartOffset := 0

	offset, found := queryOffset(data)
	if offset > 0 && found {
		identifier = string(data[0 : offset-SEPERATOR_LENGTH])
		queryStartOffset = offset
	}
	if offset > 0 && !found {
		identifier = ""
		queryStartOffset = offset
	}
	return identifier, queryStartOffset
}

func queryOffset(data []byte) (int, bool) {
	/*
		the incoming data is optionally going to start with a user-defined identifier string, followed by a ';;', and then the query payload.
		this simple helper returns the position (if any) of the FIRST ';;' found.

		the rule is that we support up to 32 characters at the start of the stream (enough to hold an MD5 hash, for example)
		so we return 0 if we have not found a ';;' within the first 32 characters, or immediately following them.
	*/
	seeking := byte(';')
	for i := range data {
		if i == 32 {
			break
		}
		if data[i] == seeking {
			// if the next char is also the same, we have found the delimiter. if not, we have an issue
			if i < len(data)-1 {
				if i == 0 && data[i+1] == seeking {
					// somehow our stream starts with ';;', so there is actually no identifier
					return i + SEPERATOR_LENGTH, false
				}
				if data[i+1] == seeking {
					return i + SEPERATOR_LENGTH, true
				}
			}
		}
	}
	return 0, false
}

func main() {
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	log.SetLevel(log.InfoLevel)

	c := getConfig()

	// set logging
	level := c.LoggingLevel
	var logLevel log.Level
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
