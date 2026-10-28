"""Runs the xai-sdk-python test server for the Go conformance tests.

Usage (from the Go tests, not by hand):

    uv run --project "$XAI_SDK_PYTHON_DIR" python run_server.py

Prints one line, "port=<n>", once the gRPC server accepts connections, then
blocks until stdin closes. The server is the one the Python SDK tests use,
so the Go client is checked against the same behaviour as the official SDK:
API key "123", static completions, four-chunk streams, tool-call outputs.
"""

import os
import sys

sys.path.insert(0, os.environ["XAI_SDK_PYTHON_DIR"])

from tests import server  # noqa: E402  (path set above)


def main() -> None:
    with server.run_test_server() as port:
        sys.stdout.write(f"port={port}\n")
        sys.stdout.flush()
        sys.stdin.read()


if __name__ == "__main__":
    main()
