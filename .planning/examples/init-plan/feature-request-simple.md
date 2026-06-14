# Feature Request: User Export

- **Estimated time**: 3 days

## Problem

Users need to export their data for backup or migration purposes. Currently there's no way to get data out of the system.

## Solution

Add a data export feature that allows users to download their data in common formats (JSON, CSV).

## Acceptance Criteria

- [ ] AC1: Users can trigger a data export via a UI button or API endpoint
- [ ] AC2: Export includes the user's profile data (name, email, preferences)
- [ ] AC3: Export includes the user's activity history (timestamped log of actions)
- [ ] AC4: Exports available in both JSON and CSV formats (user-selectable)
- [ ] AC5: Export completes within 30 seconds for accounts with typical data volumes (up to 10k activity records)
- [ ] AC6: Export contains only the requesting user's data — no other users' information included

## Success Criteria

- Users can export their profile data
- Users can export their activity history
- Exports available in JSON and CSV formats
- Export completes within 30 seconds for typical accounts

## Out of Scope

- Importing data (future feature)
- Exporting other users' data
- Scheduled/automated exports
