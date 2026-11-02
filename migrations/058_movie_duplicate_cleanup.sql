-- Explicitly planned cleanup only; migrations never schedule remote deletion.
CREATE TABLE tgstrm.movie_duplicate_cleanup (
 delivery_id uuid PRIMARY KEY REFERENCES tgstrm.movie_monitor_deliveries(id),
 keeper_id uuid NOT NULL REFERENCES tgstrm.movie_monitor_deliveries(id),
 file_id bigint NOT NULL CHECK(file_id>0),
 video_id integer NOT NULL CHECK(video_id>0),
 poster_id integer NOT NULL CHECK(poster_id>0),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','attention')),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_error text,
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(delivery_id<>keeper_id), CHECK(video_id<>poster_id)
);
