#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

OC="${OC:-oc}" # executible to verify

if test 0 -eq "${#}"
then
	printf 'Verify expected oc behavior\n\nusage: %s PATH...\n\n' "${0}" >&2
	cat <<-EOF >&2
		PATH: A directory holding test cases.  The directory is traversed for subdirectories which contain:

		* command.sh: with the shell command to execute, using \${OC:-oc} as the initial argument.
		* stdout (optional): with the expected standard output.  Defaults to "no output expected".
		* stderr (optional): with the expected standard error.  Defaults to "no output expected".
		* exit (optional): with a numerical exit code.  Defaults to 0.
		EOF
	exit 1
fi

function test_directory() {
	TEST_DIRECTORY="${1}"
	COMMAND="${TEST_DIRECTORY}/command.sh"
	if test ! -x "${COMMAND}"
	then
		return  # nothing to test
	fi

	printf 'testing %s ...\n' "${TEST_DIRECTORY}"

	EXPECTED_STDOUT=
	if test -f "${TEST_DIRECTORY}/stdout"
	then
		EXPECTED_STDOUT="$(cat "${TEST_DIRECTORY}/stdout")"
	fi
	EXPECTED_STDERR=
	if test -f "${TEST_DIRECTORY}/stderr"
	then
		EXPECTED_STDERR="$(cat "${TEST_DIRECTORY}/stderr")"
	fi
	EXPECTED_EXIT_STATUS=0
	if test -f "${TEST_DIRECTORY}/exit"
	then
		EXPECTED_EXIT_STATUS="$(cat "${TEST_DIRECTORY}/exit")"
	fi

	set +o errexit
	"${COMMAND}" > "${TEST_DIRECTORY}/tmp.stdout" 2> "${TEST_DIRECTORY}/tmp.stderr"
	ACTUAL_EXIT_STATUS="${?}"
	set -o errexit

	ACTUAL_STDOUT="$(cat "${TEST_DIRECTORY}/tmp.stdout")"
	ACTUAL_STDERR="$(cat "${TEST_DIRECTORY}/tmp.stderr")"
	rm -f "${TEST_DIRECTORY}"/tmp*

	if test "_${ACTUAL_STDERR}" != "_${EXPECTED_STDERR}"
	then
		printf "unexpected %s stderr:\n" "${TEST_DIRECTORY}"
		diff -u3 <(printf '%s' "${EXPECTED_STDERR}") <(printf '%s' "${ACTUAL_STDERR}")
		exit 1
	fi

	if test "_${ACTUAL_STDOUT}" != "_${EXPECTED_STDOUT}"
	then
		printf "unexpected %s stdout:\n" "${TEST_DIRECTORY}"
		diff -u3 <(printf '%s' "${EXPECTED_STDOUT}") <(printf '%s' "${ACTUAL_STDOUT}")
		exit 1
	fi

	if test "_${ACTUAL_EXIT_STATUS}" != "_${EXPECTED_EXIT_STATUS}"
	then
		printf "unexpected %s exit status: got %s but expected %s\n" "${TEST_DIRECTORY}" "${ACTUAL_EXIT_STATUS}" "${EXPECTED_EXIT_STATUS}"
		exit 1
	fi
}

function test_directories() {
	TEST_DIRECTORY="${1}"
	test_directory "${TEST_DIRECTORY}"

	for CHILD in "${TEST_DIRECTORY}"/*
	do
		if test -d "${CHILD}"
		then
			test_directories "${CHILD}"
		fi
	done
}

for TEST_DIRECTORY in "${@}"
do
	test_directories "${TEST_DIRECTORY}"
done
