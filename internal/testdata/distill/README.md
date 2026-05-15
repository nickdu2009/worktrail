# Distill Proposal Fixtures

These fixtures use synthetic Worktrail candidates and proposal JSON. They are
safe to copy into temporary repositories and do not depend on a real user scope,
transcript store, or local machine path.

Fixture directories mirror Worktrail state:

- `seed-candidates/<scope>/*.md` is copied into `.worktrail/candidates/<scope>/`
  or the temporary user candidate store.
- `existing-candidates/<scope>/*.md` is copied into the same candidate store
  before apply runs.
- `formal-targets/project/**` is copied into the temporary project
  `.worktrail/` root.
