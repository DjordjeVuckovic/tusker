# tusker
- Tusker is a full-text and semantic search engine for exploring multilingual news headlines and articles. The project uses Go 1.25 and focuses on importing, processing, and storing news data from various sources (currently Kaggle datasets).
- The application provides a REST API for searching and retrieving news articles based on keywords, dates, and languages. It leverages PostgreSQL and Elasticsearch for data storage and supports full-text and semantic search capabilities.
## Commands
### Migrations
- Install golang-migrate
```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```
- Each Postgres engine (`native`, `paradedb`, `tiger`) has two migration sets under `db/migrations/<engine>/`: `schema/` (tables and generated columns) and `indexes/` (one file per index, tracked in `schema_migrations_indexes`). `datapipe load` applies both itself, building the indexes after the data is in. To run them by hand against the Docker engines:
```bash
make migrate-schema-native     # or -paradedb, -tiger, -all
make migrate-indexes-native
make migrate-down-native       # drops the indexes, then the schema
```
- A failed or cancelled migration leaves its version dirty, and both `datapipe load` and `migrate` refuse the database until `migrate -path db/migrations/<engine>/<set> -database "<db>" force <version>` records the version you repaired it to (`<set>` is `schema` or `indexes`, the latter with `&x-migrations-table=schema_migrations_indexes` on the URL).
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
