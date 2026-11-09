# Troubleshoot

Start from the failure class on the work item; it's the main signal
the host passes through. The full class tables are in
[operate](operate.md).

## Troubleshooting

If a config was rejected and the host only shows you `invalid_config`,
run the binary locally with that config and a recorded request. The
stderr line names the failed step locally, while the host records only
the class. If you keep hitting `claim_deadline_exceeded` on a big
site, check the worker lease first (see
[configuration](../reference/configuration.md)); a 60-second default is rarely
enough there. If results keep failing with `result_budget_exceeded`,
raise `process.stdout_limit_bytes` and `limits.maxResultBytes`
together rather than one at a time.
