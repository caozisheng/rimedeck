-- The prior migration cannot safely distinguish old ordinary labels from
-- labels that were introduced under the complete mapping contract. Restore
-- the known canonical labels and leave all other classifications unchanged.
UPDATE issue_label
SET mapping_kind = CASE LOWER(name)
  WHEN 'workflow::backlog' THEN 'workflow'
  WHEN 'workflow::todo' THEN 'workflow'
  WHEN 'workflow::in-progress' THEN 'workflow'
  WHEN 'workflow::in-review' THEN 'workflow'
  WHEN 'workflow::done' THEN 'workflow'
  WHEN 'priority::low' THEN 'priority'
  WHEN 'priority::medium' THEN 'priority'
  WHEN 'priority::high' THEN 'priority'
  WHEN 'priority::urgent' THEN 'priority'
  WHEN 'priority::none' THEN 'none'
  ELSE mapping_kind
END
WHERE source_type = 'gitlab';
