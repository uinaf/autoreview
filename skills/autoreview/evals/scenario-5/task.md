# Handle a failed independent review

The requested provider is Claude. The branch review returned exit 2 with failure
class `authentication` before a model attempt. The configured protocol retry
count is one. Codex is also installed, and the change's tests are green.

Write `failure-report.md` with the review verdict, whether another attempt or
provider should run automatically, the safe next options for the user, and the
evidence that must not be claimed.
