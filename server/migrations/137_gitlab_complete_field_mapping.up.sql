-- Reclassify all supported canonical GitLab field labels, including the
-- explicit none/backlog values used by bidirectional synchronization.
UPDATE issue_label
SET mapping_kind = CASE LOWER(name)
  WHEN 'workflow::backlog' THEN 'workflow'
  WHEN 'workflow::todo' THEN 'workflow'
  WHEN 'workflow::in-progress' THEN 'workflow'
  WHEN 'workflow::in-review' THEN 'workflow'
  WHEN 'workflow::done' THEN 'workflow'
  WHEN 'priority::none' THEN 'priority'
  WHEN 'priority::low' THEN 'priority'
  WHEN 'priority::medium' THEN 'priority'
  WHEN 'priority::high' THEN 'priority'
  WHEN 'priority::urgent' THEN 'priority'
  ELSE mapping_kind
END
WHERE source_type = 'gitlab';
