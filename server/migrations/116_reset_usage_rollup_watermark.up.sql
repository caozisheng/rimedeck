-- Reset the hourly rollup watermark to the earliest task_usage entry
-- so all historical token data gets rolled up into task_usage_hourly.
-- The rollup function processes one day per tick (~5 min cadence), so
-- a backlog of N days catches up in ~N×5 minutes.
UPDATE task_usage_hourly_rollup_state
   SET watermark_at = COALESCE(
       (SELECT MIN(created_at) - interval '1 hour' FROM task_usage),
       now() - interval '1 day'
   )
 WHERE id = 1
   AND watermark_at > COALESCE(
       (SELECT MIN(created_at) FROM task_usage),
       now()
   );
