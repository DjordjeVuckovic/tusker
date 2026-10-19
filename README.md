# tusker
- Tusker is a full-text and semantic search engine for exploring multilingual news headlines and articles. The project uses Go 1.25 and focuses on importing, processing, and storing news data from various sources (currently Kaggle datasets).
- The application provides a REST API for searching and retrieving news articles based on keywords, dates, and languages. It leverages PostgreSQL and Elasticsearch for data storage and supports full-text and semantic search capabilities.
## Commands
### Migrations
- Install golang-migrate
```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```
- Each Postgres engine (`native`, `paradedb`, `tiger`) has a schema migration under `db/migrations/<engine>/schema/` (tables, generated columns and filter indexes) and its search indexes in `db/migrations/<engine>/indexes.sql`. `datapipe load` applies the schema, copies the data, then builds each search index as a timed stage. To migrate the schema by hand against the Docker engines:
```bash
make migrate-schema-native     # or -paradedb, -tiger, -all
make migrate-down-native       # drops the schema and every index with it
```
- A failed or cancelled schema migration leaves its version dirty, and both `datapipe load` and `migrate` refuse the database until `migrate -path db/migrations/<engine>/schema -database "<db>" force <version>` records the version you repaired it to.
### OpenAPI Schema gen
- Install swag CLI
```bash
go install github.com/swaggo/swag/cmd/swag@latest
```
- Generate OpenAPI spec
```bash
swag init -g cmd/news_api/main.go -o ./api/openapi-spec
```
### Run formatter
```bash
go fmt ./...
```

### Run static analysis
```bash
go vet ./...
```

### Run tests
```bash
go test ./...
```
### Run linter
- Install golangci-lint
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```
```bash
golangci-lint run
```
