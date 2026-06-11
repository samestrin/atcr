# Active Sprints

This directory contains the current and future sprint plans organized in nested folder structures.

## Usage

- Use `/create-sprint [path/to/plan-folder]` to generate new actionable sprint plans
- Each sprint gets its own folder with organized subdirectories
- Update progress regularly in the sprint's README.md
- Move completed sprint folders to `../completed/`

## Folder Structure

- `{SPRINT_NUMBER}_{feature-name}/` - Main sprint folder
  - `README.md` - Main sprint plan and progress tracking
  - `sprint-plan.md` - Actionable step-by-step Sprint Plan with integrated tracking
  - `metadata.md` - Source of truth for sprint metadata and state, including start/end dates, team assignments, and dependencies
  - `documentation/` - Supporting documentation, API specs, design specs, and guidelines
  AND
  - `user-stories/` - User-centric requirements and scenarios
  - `acceptance-criteria/` - Detailed conditions for task verification with integrated tracking
  OR
  - `tasks/` - Technical implementation steps and debt items with integrated tracking

- Example: `7.0_user_authentication/README.md`

## Current Active Sprints

| Sprint | Type | Status | Description |
|--------|------|--------|-------------|
| [108.0_ui_performance_integration](108.0_ui_performance_integration/) | Tech Debt | Ready | Integrate UI performance components |
| [109.0_ux_visual_feedback_fixes](109.0_ux_visual_feedback_fixes/) | Bug Fix | Ready | Fix clipboard and toast feedback |
| [110.0_visual_collaboration_completion](110.0_visual_collaboration_completion/) | Feature | Ready | Complete visual collaboration feature |
| [111.0_voice_profile_list_delete](111.0_voice_profile_list_delete/) | Feature | Ready | Add delete button to voice profile list view |
| [112.0_ai_service_infrastructure](112.0_ai_service_infrastructure/) | Infrastructure | Ready | Improve AI service reliability and observability |
| [113.0_test_automation_gaps](113.0_test_automation_gaps/) | Test Remediation | Ready | Add error state tests and document testing strategy |

## File Organization

- **All sprint files must be organized within the sprint folder**
- Do not create files in the project root during sprint execution
- Use the appropriate subfolder for different types of content
