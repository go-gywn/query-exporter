# query-exporter

1. Centralized query execution
2. Target DB group management
3. Flexible metric definition

## Build & Run
```bash
go get github.com/go-gywn/query-exporter
cd $GOPATH/src/github.com/go-gywn/query-exporter
go build .
./query-exporter                          \
  --address="0.0.0.0:9104"                \
  --config-database="config-database.yml" \
  --config-metrics="config-metrics.yml"
```
## config-database format
```yaml
prod:
  prod01:
    type: mysql
    dsn: tcp(127.0.0.1:3306)/information_schema
    user: test
    pass: test123
  prod02:
    type: mysql
    dsn: tcp(127.0.0.1:3306)/information_schema
    user: test
    pass: test123
dev:
  dev01:
    type: mysql
    dsn: tcp(127.0.0.1:3306)/information_schema
    user: test
    pass: test123
```
## config-metrics format
```yaml
metric01:
  targets: ["prod"]
  collects:
  - query: "select user, 
                   substring_index(host, ':', 1) host,
                   db,
                   command,
                   count(*) sessions,
                   min(time) min_time,
                   max(time) max_time
            from information_schema.processlist
            group by 1,2,3,4"
    metrics:
      process_count:
        type: guage
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "sessions"
      process_session_min_time:
        type: guage
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "min_time"
      process_session_max_time:
        type: guage
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "max_time"
  - query: "select count(*) cnt from information_schema.innodb_trx"
    metrics:
      innodb_trx_count:
        type: guage
        description: innodb current trx count
        labels: []
        value: "cnt"
metric02:
  targets: ["dev"]
  collects:
  - query: "select user, 
                   substring_index(host, ':', 1) host,
                   db,
                   command,
                   count(*) sessions,
                   min(time) min_time,
                   max(time) min_time
            from information_schema.processlist
            group by 1,2,3,4"
    metrics:
      process_count:
        type: guage
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "sessions"
      process_session_min_time:
        type: guage
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "min_time"
      process_session_max_time:
        type: guage
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "max_time"
```
You can check with this url
```
curl 127.0.0.1:9104/metric01
curl 127.0.0.1:9104/metric02
```
