-- Keep the per-workspace issue counter ahead of all existing issue numbers.
-- Older import paths allocated numbers from MAX(number)+1 without advancing
-- this counter, so the next normal create could reuse an existing number.
UPDATE workspace w
SET issue_counter = GREATEST(
  w.issue_counter,
  COALESCE((SELECT MAX(i.number) FROM issue i WHERE i.workspace_id = w.id), 0)
);
