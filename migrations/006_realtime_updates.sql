CREATE TABLE tgstrm.telegram_update_states (
 account_id uuid PRIMARY KEY REFERENCES tgstrm.telegram_accounts(id) ON DELETE CASCADE,
 telegram_user_id bigint NOT NULL,
 pts integer NOT NULL, qts integer NOT NULL, update_date integer NOT NULL, seq integer NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tgstrm.telegram_channel_states (
 account_id uuid NOT NULL REFERENCES tgstrm.telegram_accounts(id) ON DELETE CASCADE,
 telegram_user_id bigint NOT NULL, telegram_channel_id bigint NOT NULL,
 pts integer, access_hash bigint,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(account_id, telegram_user_id, telegram_channel_id)
);

CREATE TABLE tgstrm.outbox_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 event_type text NOT NULL,
 aggregate_type text NOT NULL,
 aggregate_id bigint NOT NULL,
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 delivered_at timestamptz
);
CREATE INDEX outbox_pending ON tgstrm.outbox_events(id) WHERE delivered_at IS NULL;
