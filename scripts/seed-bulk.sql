\set ON_ERROR_STOP on
SET client_min_messages = warning;
\if :{?rows}
\else
  \set rows 1000000
\endif

DROP TABLE IF EXISTS public.training_sessions;

CREATE TABLE public.training_sessions (
	id             bigint PRIMARY KEY,
	trainee_id     integer NOT NULL REFERENCES public.trainees(id),
	target_race_id integer REFERENCES public.races(id),
	session_date   date NOT NULL,
	training_type  text NOT NULL CHECK (training_type IN ('Speed', 'Stamina', 'Power', 'Guts', 'Wit')),
	mood           text NOT NULL CHECK (mood IN ('Great', 'Good', 'Normal', 'Bad', 'Awful')),
	stat_gain      smallint NOT NULL CHECK (stat_gain > 0),
	energy_cost    smallint NOT NULL,
	report_status  text NOT NULL DEFAULT 'draft' CHECK (report_status IN ('draft', 'published')),
	updated_at     timestamptz NOT NULL DEFAULT now()
);

SELECT setseed(0.42);

WITH plan AS (
	SELECT
		t.id AS trainee_id,
		array_agg(r.id ORDER BY r.id) AS race_ids
	FROM public.trainees AS t
	LEFT JOIN public.races AS r
		ON r.surface = t.preferred_surface
		AND CASE
			WHEN r.distance_m <= 1400 THEN 'Sprint'
			WHEN r.distance_m <= 1800 THEN 'Mile'
			WHEN r.distance_m <= 2400 THEN 'Medium'
			ELSE 'Long'
		END = t.preferred_distance
	GROUP BY t.id
),
roster AS (
	SELECT row_number() OVER (ORDER BY trainee_id) - 1 AS slot, trainee_id, race_ids
	FROM plan
),
roster_size AS (
	SELECT count(*) AS n FROM roster
),
sessions AS (
	SELECT
		g AS id,
		roster.trainee_id,
		roster.race_ids[(1 + (g / roster_size.n) % cardinality(roster.race_ids))::int] AS target_race_id,
		DATE '2025-01-01' + ((g / roster_size.n) % 545)::int AS session_date,
		(ARRAY['Speed', 'Stamina', 'Power', 'Guts', 'Wit'])[1 + floor(random() * 5)::int] AS training_type,
		(ARRAY['Great', 'Good', 'Normal', 'Bad', 'Awful'])[1 + floor(random() * 5)::int] AS mood
	FROM generate_series(1, :rows) AS g
	CROSS JOIN roster_size
	JOIN roster ON roster.slot = g % roster_size.n
)
INSERT INTO public.training_sessions
	(id, trainee_id, target_race_id, session_date, training_type, mood, stat_gain, energy_cost)
SELECT
	id,
	trainee_id,
	target_race_id,
	session_date,
	training_type,
	mood,
	(CASE mood WHEN 'Great' THEN 18 WHEN 'Good' THEN 15 WHEN 'Normal' THEN 12 WHEN 'Bad' THEN 9 ELSE 6 END
		+ floor(random() * 8))::smallint,
	CASE WHEN training_type = 'Wit' THEN -5 ELSE (15 + floor(random() * 16))::smallint END
FROM sessions;

CREATE INDEX training_sessions_trainee_id_idx ON public.training_sessions (trainee_id);

VACUUM ANALYZE public.training_sessions;

SELECT
	count(*) AS rows,
	count(DISTINCT trainee_id) AS trainees,
	pg_size_pretty(pg_total_relation_size('public.training_sessions')) AS table_size
FROM public.training_sessions;
