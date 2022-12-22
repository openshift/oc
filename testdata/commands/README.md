Example commands and their expected output, for verifying 'oc' invocations against expected output.
We could use [an off the shelf harness][TAP-shell], but I'm opening with something local to get started.
Process with [`verify-commands.sh`](/hack/verify-commands.sh).
Subdirectories contain:

* `command.sh`: with the shell command to execute, using `${OC:-oc}` as the initial argument.
* `stdout` (optional): with the expected standard output.  Defaults to "no output expected".
* `stderr` (optional): with the expected standard error.  Defaults to "no output expected".
* `exit` (optional): with a numerical exit code.  Defaults to 0.

[TAP-shell]: https://testanything.org/producers.html#shell
