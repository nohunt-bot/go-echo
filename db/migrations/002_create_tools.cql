USE go_echo;

CREATE TABLE IF NOT EXISTS tools (
    id          uuid PRIMARY KEY,
    name        text,
    description text,
    category    text,
    status      text
);

CREATE INDEX IF NOT EXISTS tools_category_idx ON tools (category);
CREATE INDEX IF NOT EXISTS tools_status_idx   ON tools (status);
