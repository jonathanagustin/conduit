# ledger-api

An Express API that keeps ledger entries in PostgreSQL.

- Listens on port 3000.
- Requires the `DATABASE_URL` environment variable (a PostgreSQL connection
  string). The service does not start without it.
- The database is not part of this repository. Which PostgreSQL instance the
  service should use in a given environment is a deployment decision.

Run with `npm start`.
