-- Development-only seed for the managed target database: a Tracen Academy
-- record of trainees, teams, G1 races, and race results.
-- Run this against targetdb, not the platform database. Safe to run again.

BEGIN;

CREATE TABLE IF NOT EXISTS public.teams (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO public.teams (name)
VALUES ('Spica'), ('Rigil'), ('Canopus')
ON CONFLICT (name) DO NOTHING;

CREATE TABLE IF NOT EXISTS public.trainees (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    team_id INTEGER REFERENCES public.teams(id),
    running_style TEXT NOT NULL
        CHECK (running_style IN ('Front Runner', 'Pace Chaser', 'Late Surger', 'End Closer')),
    preferred_distance TEXT NOT NULL
        CHECK (preferred_distance IN ('Sprint', 'Mile', 'Medium', 'Long')),
    preferred_surface TEXT NOT NULL DEFAULT 'Turf'
        CHECK (preferred_surface IN ('Turf', 'Dirt')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS trainees_team_id_idx ON public.trainees (team_id);

INSERT INTO public.trainees (name, team_id, running_style, preferred_distance, preferred_surface)
SELECT
    seed.name,
    (SELECT id FROM public.teams WHERE name = seed.team),
    seed.running_style,
    seed.preferred_distance,
    seed.preferred_surface
FROM (
    VALUES
        ('Special Week', 'Spica', 'Late Surger', 'Medium', 'Turf'),
        ('Silence Suzuka', 'Spica', 'Front Runner', 'Medium', 'Turf'),
        ('Tokai Teio', 'Spica', 'Pace Chaser', 'Medium', 'Turf'),
        ('Mejiro McQueen', 'Spica', 'Pace Chaser', 'Long', 'Turf'),
        ('Gold Ship', 'Spica', 'End Closer', 'Long', 'Turf'),
        ('Vodka', 'Spica', 'Late Surger', 'Mile', 'Turf'),
        ('Daiwa Scarlet', 'Spica', 'Front Runner', 'Mile', 'Turf'),
        ('Symboli Rudolf', 'Rigil', 'Pace Chaser', 'Long', 'Turf'),
        ('Air Groove', 'Rigil', 'Pace Chaser', 'Medium', 'Turf'),
        ('Maruzensky', 'Rigil', 'Front Runner', 'Mile', 'Turf'),
        ('Taiki Shuttle', 'Rigil', 'Pace Chaser', 'Mile', 'Turf'),
        ('Grass Wonder', 'Rigil', 'Pace Chaser', 'Medium', 'Turf'),
        ('El Condor Pasa', 'Rigil', 'Pace Chaser', 'Medium', 'Turf'),
        ('Fuji Kiseki', 'Rigil', 'Pace Chaser', 'Mile', 'Turf'),
        ('Narita Brian', 'Rigil', 'Pace Chaser', 'Long', 'Turf'),
        ('Hishi Amazon', 'Rigil', 'End Closer', 'Mile', 'Turf'),
        ('Nice Nature', 'Canopus', 'Late Surger', 'Medium', 'Turf'),
        ('Twin Turbo', 'Canopus', 'Front Runner', 'Medium', 'Turf'),
        ('Ikuno Dictus', 'Canopus', 'Pace Chaser', 'Medium', 'Turf'),
        ('Matikane Tannhauser', 'Canopus', 'Pace Chaser', 'Medium', 'Turf'),
        ('Oguri Cap', NULL, 'Pace Chaser', 'Mile', 'Turf'),
        ('Tamamo Cross', NULL, 'End Closer', 'Long', 'Turf'),
        ('Super Creek', NULL, 'Pace Chaser', 'Long', 'Turf'),
        ('Inari One', NULL, 'Late Surger', 'Long', 'Turf'),
        ('Seiun Sky', NULL, 'Front Runner', 'Long', 'Turf'),
        ('King Halo', NULL, 'Late Surger', 'Sprint', 'Turf'),
        ('Rice Shower', NULL, 'Pace Chaser', 'Long', 'Turf'),
        ('Mihono Bourbon', NULL, 'Front Runner', 'Medium', 'Turf'),
        ('Biwa Hayahide', NULL, 'Pace Chaser', 'Long', 'Turf'),
        ('Winning Ticket', NULL, 'Late Surger', 'Medium', 'Turf'),
        ('Mayano Top Gun', NULL, 'Pace Chaser', 'Long', 'Turf'),
        ('T.M. Opera O', NULL, 'Pace Chaser', 'Medium', 'Turf'),
        ('Meisho Doto', NULL, 'Pace Chaser', 'Medium', 'Turf'),
        ('Admire Vega', NULL, 'Late Surger', 'Medium', 'Turf'),
        ('Agnes Tachyon', NULL, 'Pace Chaser', 'Medium', 'Turf'),
        ('Manhattan Cafe', NULL, 'End Closer', 'Long', 'Turf'),
        ('Agnes Digital', NULL, 'Late Surger', 'Mile', 'Dirt'),
        ('Smart Falcon', NULL, 'Front Runner', 'Mile', 'Dirt'),
        ('Haru Urara', NULL, 'Late Surger', 'Sprint', 'Dirt'),
        ('Yukino Bijin', NULL, 'Pace Chaser', 'Mile', 'Dirt'),
        ('Sakura Bakushin O', NULL, 'Front Runner', 'Sprint', 'Turf'),
        ('Curren Chan', NULL, 'Pace Chaser', 'Sprint', 'Turf'),
        ('Biko Pegasus', NULL, 'Pace Chaser', 'Sprint', 'Turf'),
        ('Nishino Flower', NULL, 'Pace Chaser', 'Mile', 'Turf'),
        ('Daitaku Helios', NULL, 'Front Runner', 'Mile', 'Turf'),
        ('Mejiro Palmer', NULL, 'Front Runner', 'Long', 'Turf'),
        ('Mejiro Ryan', NULL, 'Late Surger', 'Medium', 'Turf'),
        ('Mejiro Dober', NULL, 'Late Surger', 'Mile', 'Turf'),
        ('Matikanefukukitaru', NULL, 'Late Surger', 'Long', 'Turf'),
        ('Eishin Flash', NULL, 'Late Surger', 'Medium', 'Turf'),
        ('Kawakami Princess', NULL, 'Late Surger', 'Medium', 'Turf'),
        ('Gold City', NULL, 'Late Surger', 'Medium', 'Turf'),
        ('Zenno Rob Roy', NULL, 'Late Surger', 'Long', 'Turf'),
        ('Narita Taishin', NULL, 'End Closer', 'Medium', 'Turf'),
        ('Tosen Jordan', NULL, 'Pace Chaser', 'Medium', 'Turf'),
        ('Kitasan Black', NULL, 'Front Runner', 'Long', 'Turf'),
        ('Satono Diamond', NULL, 'Pace Chaser', 'Long', 'Turf'),
        ('Sakura Chiyono O', NULL, 'Pace Chaser', 'Medium', 'Turf'),
        ('Sirius Symboli', NULL, 'Pace Chaser', 'Medium', 'Turf'),
        ('Mr. C.B.', NULL, 'Pace Chaser', 'Medium', 'Turf')
) AS seed(name, team, running_style, preferred_distance, preferred_surface)
ON CONFLICT (name) DO NOTHING;

CREATE TABLE IF NOT EXISTS public.races (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    grade TEXT NOT NULL CHECK (grade IN ('G1', 'G2', 'G3')),
    racecourse TEXT NOT NULL,
    surface TEXT NOT NULL CHECK (surface IN ('Turf', 'Dirt')),
    distance_m INTEGER NOT NULL CHECK (distance_m > 0)
);

INSERT INTO public.races (name, grade, racecourse, surface, distance_m)
VALUES
    ('Takamatsunomiya Kinen', 'G1', 'Chukyo', 'Turf', 1200),
    ('Osaka Hai', 'G1', 'Hanshin', 'Turf', 2000),
    ('Oka Sho', 'G1', 'Hanshin', 'Turf', 1600),
    ('Satsuki Sho', 'G1', 'Nakayama', 'Turf', 2000),
    ('Tenno Sho (Spring)', 'G1', 'Kyoto', 'Turf', 3200),
    ('Japanese Oaks', 'G1', 'Tokyo', 'Turf', 2400),
    ('Japanese Derby', 'G1', 'Tokyo', 'Turf', 2400),
    ('Yasuda Kinen', 'G1', 'Tokyo', 'Turf', 1600),
    ('Takarazuka Kinen', 'G1', 'Hanshin', 'Turf', 2200),
    ('Sprinters Stakes', 'G1', 'Nakayama', 'Turf', 1200),
    ('Kikuka Sho', 'G1', 'Kyoto', 'Turf', 3000),
    ('Tenno Sho (Autumn)', 'G1', 'Tokyo', 'Turf', 2000),
    ('Mile Championship', 'G1', 'Kyoto', 'Turf', 1600),
    ('Japan Cup', 'G1', 'Tokyo', 'Turf', 2400),
    ('Champions Cup', 'G1', 'Chukyo', 'Dirt', 1800),
    ('Hopeful Stakes', 'G1', 'Nakayama', 'Turf', 2000),
    ('Arima Kinen', 'G1', 'Nakayama', 'Turf', 2500),
    ('February Stakes', 'G1', 'Tokyo', 'Dirt', 1600)
ON CONFLICT (name) DO NOTHING;

CREATE TABLE IF NOT EXISTS public.race_results (
    id SERIAL PRIMARY KEY,
    race_id INTEGER NOT NULL REFERENCES public.races(id),
    trainee_id INTEGER NOT NULL REFERENCES public.trainees(id),
    run_on DATE NOT NULL,
    finish_position INTEGER NOT NULL CHECK (finish_position > 0),
    finish_time_seconds NUMERIC(5, 1) NOT NULL CHECK (finish_time_seconds > 0),
    UNIQUE (race_id, trainee_id, run_on)
);

CREATE INDEX IF NOT EXISTS race_results_trainee_id_idx ON public.race_results (trainee_id);

INSERT INTO public.race_results (race_id, trainee_id, run_on, finish_position, finish_time_seconds)
SELECT
    (SELECT id FROM public.races WHERE name = seed.race),
    (SELECT id FROM public.trainees WHERE name = seed.trainee),
    seed.run_on::date,
    seed.finish_position,
    seed.finish_time_seconds
FROM (
    VALUES
        ('Oka Sho', '2025-04-06', 'Daiwa Scarlet', 1, 93.9),
        ('Oka Sho', '2025-04-06', 'Nishino Flower', 2, 94.0),
        ('Oka Sho', '2025-04-06', 'Vodka', 3, 94.2),
        ('Satsuki Sho', '2025-04-20', 'Seiun Sky', 1, 118.8),
        ('Satsuki Sho', '2025-04-20', 'King Halo', 2, 119.0),
        ('Satsuki Sho', '2025-04-20', 'Special Week', 3, 119.1),
        ('Tenno Sho (Spring)', '2025-05-04', 'Rice Shower', 1, 197.1),
        ('Tenno Sho (Spring)', '2025-05-04', 'Mejiro McQueen', 2, 197.5),
        ('Tenno Sho (Spring)', '2025-05-04', 'Gold Ship', 3, 197.9),
        ('Japanese Derby', '2025-06-01', 'Special Week', 1, 145.2),
        ('Japanese Derby', '2025-06-01', 'Seiun Sky', 2, 145.9),
        ('Japanese Derby', '2025-06-01', 'King Halo', 3, 146.1),
        ('Takarazuka Kinen', '2025-06-29', 'Silence Suzuka', 1, 131.3),
        ('Takarazuka Kinen', '2025-06-29', 'Air Groove', 2, 131.8),
        ('Takarazuka Kinen', '2025-06-29', 'Mejiro McQueen', 3, 132.0),
        ('Sprinters Stakes', '2025-10-05', 'Sakura Bakushin O', 1, 67.9),
        ('Sprinters Stakes', '2025-10-05', 'Curren Chan', 2, 68.1),
        ('Sprinters Stakes', '2025-10-05', 'Biko Pegasus', 3, 68.3),
        ('Kikuka Sho', '2025-10-26', 'Seiun Sky', 1, 183.2),
        ('Kikuka Sho', '2025-10-26', 'Special Week', 2, 183.7),
        ('Kikuka Sho', '2025-10-26', 'Matikanefukukitaru', 3, 184.0),
        ('Tenno Sho (Autumn)', '2025-11-02', 'Vodka', 1, 117.2),
        ('Tenno Sho (Autumn)', '2025-11-02', 'Daiwa Scarlet', 2, 117.2),
        ('Tenno Sho (Autumn)', '2025-11-02', 'Air Groove', 3, 117.6),
        ('Mile Championship', '2025-11-23', 'Taiki Shuttle', 1, 92.8),
        ('Mile Championship', '2025-11-23', 'Daitaku Helios', 2, 93.0),
        ('Mile Championship', '2025-11-23', 'Nishino Flower', 3, 93.4),
        ('Japan Cup', '2025-11-30', 'El Condor Pasa', 1, 145.0),
        ('Japan Cup', '2025-11-30', 'Air Groove', 2, 145.3),
        ('Japan Cup', '2025-11-30', 'Special Week', 3, 145.4),
        ('Hopeful Stakes', '2025-12-27', 'Agnes Tachyon', 1, 121.1),
        ('Hopeful Stakes', '2025-12-27', 'Manhattan Cafe', 2, 121.4),
        ('Arima Kinen', '2025-12-28', 'Grass Wonder', 1, 152.0),
        ('Arima Kinen', '2025-12-28', 'Special Week', 2, 152.0),
        ('Arima Kinen', '2025-12-28', 'Tokai Teio', 3, 152.4),
        ('Arima Kinen', '2025-12-28', 'Gold Ship', 4, 152.6),
        ('February Stakes', '2026-02-22', 'Smart Falcon', 1, 95.6),
        ('February Stakes', '2026-02-22', 'Agnes Digital', 2, 95.8),
        ('February Stakes', '2026-02-22', 'Haru Urara', 16, 99.1),
        ('Osaka Hai', '2026-04-05', 'Kitasan Black', 1, 117.9),
        ('Osaka Hai', '2026-04-05', 'Satono Diamond', 2, 118.2),
        ('Osaka Hai', '2026-04-05', 'Mihono Bourbon', 3, 118.4),
        ('Tenno Sho (Spring)', '2026-05-03', 'Kitasan Black', 1, 192.5),
        ('Tenno Sho (Spring)', '2026-05-03', 'Gold Ship', 2, 193.0),
        ('Tenno Sho (Spring)', '2026-05-03', 'Satono Diamond', 3, 193.1),
        ('Takarazuka Kinen', '2026-06-28', 'Gold Ship', 1, 131.9),
        ('Takarazuka Kinen', '2026-06-28', 'Mejiro Palmer', 2, 132.2),
        ('Takarazuka Kinen', '2026-06-28', 'T.M. Opera O', 3, 132.4)
) AS seed(race, run_on, trainee, finish_position, finish_time_seconds)
ON CONFLICT (race_id, trainee_id, run_on) DO NOTHING;

CREATE OR REPLACE VIEW public.race_winners AS
SELECT
    result.run_on,
    race.name AS race,
    race.racecourse,
    race.distance_m,
    trainee.name AS winner,
    team.name AS team,
    result.finish_time_seconds
FROM public.race_results AS result
JOIN public.races AS race ON race.id = result.race_id
JOIN public.trainees AS trainee ON trainee.id = result.trainee_id
LEFT JOIN public.teams AS team ON team.id = trainee.team_id
WHERE result.finish_position = 1
ORDER BY result.run_on, race.name;

COMMIT;