-- 002_decision_weights.sql
--
-- Add the `weights` column to `decisions` so the scoring snapshot
-- persists alongside Inputs / Outcome. Lets a replay across policy
-- changes diff the new score against the historical weights without
-- relying on the code path that produced the original score.
--
-- JSON-encoded `map[string]float64`. NULL on rows older than this
-- migration; readers tolerate the absence.

ALTER TABLE decisions ADD COLUMN weights TEXT;
