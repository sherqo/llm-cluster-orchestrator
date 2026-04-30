Here, you can find the documentation for the master component of our system. The master is responsible for managing the overall operation of the cluster, including task distribution, worker management, and system monitoring.

will be written in `golang`

we have 2 DB in the design or at least 2 tables

I think I can go with `SQLite`.

# How to

## upgrade dependencies

```bash
go get -u
```

## run the master

```bash
go run main.go
```
