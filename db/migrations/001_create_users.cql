-- Run once against your Cassandra cluster before starting the server.

CREATE KEYSPACE IF NOT EXISTS go_echo
    WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

USE go_echo;

CREATE TABLE IF NOT EXISTS users (
    id    uuid PRIMARY KEY,
    name  text,
    email text
);

CREATE INDEX IF NOT EXISTS users_email_idx ON users (email);
