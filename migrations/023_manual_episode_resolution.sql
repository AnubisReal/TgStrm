CREATE TABLE tgstrm.stream_episode_overrides (
 asset_id uuid PRIMARY KEY REFERENCES tgstrm.stream_assets(id) ON DELETE CASCADE,
 series_tmdb_id bigint NOT NULL CHECK (series_tmdb_id > 0),
 season_number integer NOT NULL CHECK (season_number >= 0),
 episode_number integer NOT NULL CHECK (episode_number > 0),
 episode_title text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX stream_episode_overrides_identity
 ON tgstrm.stream_episode_overrides(series_tmdb_id,season_number,episode_number);
