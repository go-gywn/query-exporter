# query-exporter

1. Centralized query execution
2. Target DB group management
3. Flexible metric definition

## Build & Run
```bash
go build .
./query-exporter                          \
  --threads=8                             \
  --bind="0.0.0.0:9104"                   \
  --config-database="config-database.yml" \
  --config-metrics="config-metrics.yml"
```

## Debugging
```bash
export LOG_LEVEL="debug" 
./query-exporter                          \
  --threads=8                             \
  --bind="0.0.0.0:9104"                   \
  --config-database="config-database.yml" \
  --config-metrics="config-metrics.yml"
```

## config-database format
```yaml
prod:
  prod01:
    type: mysql
    dsn: test:test123@tcp(127.0.0.1:3306)/information_schema
  prod02:
    type: mysql
    dsn: test:test123@tcp(127.0.0.1:3306)/information_schema
dev:
  dev01:
    type: mysql
    dsn: test:test123@tcp(127.0.0.1:3306)/information_schema
```
### ## database drivers
1. MySQL
  https://github.com/go-sql-driver/mysql
2. Oracle
  https://github.com/godror/godror
3. Postgres
  https://github.com/lib/pq
4. SQLite
  https://github.com/mattn/go-sqlite3
5. MS-SQL
  https://github.com/denisenkom/go-mssqldb

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
    timeout: 1
    metrics:
      process_count:
        type: gauge
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "sessions"
      process_session_min_time:
        type: gauge
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "min_time"
      process_session_max_time:
        type: gauge
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "max_time"
  - query: "select count(*) cnt from information_schema.innodb_trx"
    metrics:
      innodb_trx_count:
        type: gauge
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
    timeout: 2
    metrics:
      process_count:
        type: gauge
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "sessions"
      process_session_min_time:
        type: gauge
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "min_time"
      process_session_max_time:
        type: gauge
        description: Session count
        labels: ["user","host", "db", "command"]
        value: "max_time"
```
You can check with this url
```
curl 127.0.0.1:9104/metric01
curl 127.0.0.1:9104/metric02
```
