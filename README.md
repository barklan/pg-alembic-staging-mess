it's a mess:

- Cannot be containerized
- Pg+alembic specific
- Uses tricks and hacks to parse alembic tree and difficult to test
- Assumes existence of mythical `db_dump_stag.sql` at specific path and that it is regularly updated by some third party
- Cannot work without shell (even worse, it's bash specific)
- Writes `.env` file to manage state
- Virtually every path and container name is hardcoded
