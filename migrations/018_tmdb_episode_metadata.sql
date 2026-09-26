CREATE TABLE tgstrm.tmdb_episode_metadata (
 series_tmdb_id bigint NOT NULL CHECK (series_tmdb_id > 0),
 season_number integer NOT NULL CHECK (season_number >= 0),
 episode_number integer NOT NULL CHECK (episode_number > 0),
 language text NOT NULL DEFAULT 'es-ES',
 title text NOT NULL,
 metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
 fetched_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(series_tmdb_id,season_number,episode_number,language)
);
