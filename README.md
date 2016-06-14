#Go client to lock server

Golang client library for reliable distributed lock-servers.

----------

##Example usage

Start mock-servers:

    $ go run mosk_server1.go &
    $ go run mosk_server2.go &
    $ go run mosk_server3.go &


Start main.go for go-client load testing:

    $ go run main.go
 
or

    $ go run -race main.go
