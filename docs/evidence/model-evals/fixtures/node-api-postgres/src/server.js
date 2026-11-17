// ledger-api stores entries in PostgreSQL. DATABASE_URL is required; the
// process exits when it is missing.
const express = require("express");
const { Pool } = require("pg");

if (!process.env.DATABASE_URL) {
  console.error("DATABASE_URL is required");
  process.exit(1);
}

const pool = new Pool({ connectionString: process.env.DATABASE_URL });
const app = express();
app.use(express.json());

app.get("/entries", async (_req, res) => {
  const { rows } = await pool.query("SELECT id, amount FROM entries ORDER BY id");
  res.json(rows);
});

app.post("/entries", async (req, res) => {
  await pool.query("INSERT INTO entries (amount) VALUES ($1)", [req.body.amount]);
  res.status(201).end();
});

app.listen(3000);
