[![codecov](https://codecov.io/gh/nicgrobler/sql3net/branch/master/graph/badge.svg?token=MI697Wb3tt)](https://codecov.io/gh/nicgrobler/sql3net)
[![License](https://img.shields.io/cocoapods/l/ImageCoordinateSpace.svg?style=flat)](http://cocoapods.org/pods/ImageCoordinateSpace)
[![Actions Status](https://github.com/nicgrobler/sql3net/workflows/Test/badge.svg)](https://github.com/nicgrobler/sql3net/actions)
# SQL3net

A basic wrapper around the excellent SQLite3 library that includes a network server which allows SQLite to be used as if it is running on the local machine. This can be very useful when deployed into its own container, which will, by default, allow all other containers within the same network to share a fully SQL compliant DB without any additional configuration. The server handles locking to enable SQLite to handle multiple concurrent access - many readers do not block each other, only writes will lock the db. This effectively adds a basic form of concurrency to SQLite, which doesn't support this.  

It can run as a simple stand-alone app, or as a docker container - where it will by default be usable from other containers. This means that you can use "SQLite as a service" within your docker environment easily.  

The SQL3net tool opens to listeners:  
HTTP -> port 9090  
TCP  -> port 3030  

*This code works by running the docker-compose file, or, by simply building it on your host, and invoking it directly (but this will require that you have the sqlite3 libs on your host)*  

### Run
The tool uses environment variables which are supplied from the *config.env* file (and the docker-compose file if running as container).
simply call the code like this:
```
$ source ./config.env
$ sql3net
```
or, using docker-compose:
```
docker-compose build && docker-compose up -d
```
## raw network port
It is possible to send queries directly over raw TCP - this can be done by piping the query to the socket using Unix pipes, or, by piping a file containing the queries. for example:

```
$ cat create_insert.txt
create table bubble(id int, word text);
insert into bubble(id, word) values (1,"banana");
insert into bubble(id, word) values (1,"banana");
insert into bubble(id, word) values (10,"oranges");
insert into bubble(id, word) values (111,"apples");
```
then insert:
```
$ nc -q 2 localhost 3030 < create_insert.txt
```
then read back out:
```
$ nc -q 2 localhost 3030 < query.txt
1|banana
1|banana
10|oranges
111|apples
```
and using NC (Netcat) to do a basic query:
```
$ echo "select * from bubbles" | nc localhost 3030
```
## HTTP port
a second method is to use HTTP as the transport:

```
$ curl --data-binary "@/create_insert.txt" http://localhost:9090/write
```
then query it back:

```
$ curl --data-binary "@/query.txt" http://localhost:9090/read
1|banana
1|banana
10|oranges
111|apples
```

### Docker
simply run the container like this:
```
$ docker-compose build && docker-compose up -d
```

the "syntax" is exactly the same, except that you can talk to the container by referencing its name:
```
$ echo "select * from bubbles" | nc sql3net 3030
```

Although the examples use netcat, you can use *any* network client able to send tcp packets to a given address. There is no serialization taking place, just send and receive of raw bytes - these are then forwarded to SQLite exactly as would be the case if you ran your queries against a local SQLite file. There are no additonal controls - you are free to use and abuse the db as you need.

NOTE: by *default* SQL3net creates a db file based on the network address of the *source* of the connection, but this can be overriden at runtime by simply supplying your own identifer - this can be any string of up-to 32 valid characters, followed by ";;", and then the SQL statement. For example:  
```
$ cat create_insert.txt
a1b2-c3d4-something-funky;;create table bubble(id int, word text);
insert into bubble(id, word) values (1,"banana");
insert into bubble(id, word) values (1,"banana");
insert into bubble(id, word) values (10,"oranges");
insert into bubble(id, word) values (111,"apples");
```

which will, when sent to SQL3net, create a db file named "a1b2-c3d4-something-funky.db". prepend the same identifer at query time to retreive your data:
```
$ cat query.txt
a1b2c3d4;;select * from bubble;
```

