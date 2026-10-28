"""Runs the xai-sdk-python test server for the Go conformance tests.

Usage (from the Go tests, not by hand):

    uv run --project "$XAI_SDK_PYTHON_DIR" python run_server.py

Prints "port=<n>" for the API server, "mgmt_port=<n>" for the management
server (collections) and "flaky_port=<n>" / "flaky_port_2=<n>" for two servers
whose first call fails with UNAVAILABLE, and "slow_port=<n>" for one that
answers chat and auth calls after two seconds, once all accept connections, then blocks until stdin
closes. Both share one in-memory store, as the Python tests do, so a file
uploaded through the API server can be added to a collection. The server is the one the Python SDK tests use,
so the Go client is checked against the same behaviour as the official SDK:
API key "123", static completions, four-chunk streams, tool-call outputs.
"""

import os
import sys

sys.path.insert(0, os.environ["XAI_SDK_PYTHON_DIR"])

from tests import server  # noqa: E402  (path set above)


def main() -> None:
    store = server.InMemoryStore()
    # Two servers whose first Auth call fails with UNAVAILABLE: one for the
    # retry test, one for the retries-disabled test (the failure is consumed
    # by the first call, so each needs its own).
    with server.run_test_server(in_memory_store=store) as port, server.run_test_management_server(
        in_memory_store=store
    ) as mgmt_port, server.run_test_server(initial_failures=1) as flaky_port, server.run_test_server(
        initial_failures=1
    ) as flaky_port_2, server.run_test_server(response_delay_seconds=2) as slow_port:
        sys.stdout.write(
            f"port={port}\nmgmt_port={mgmt_port}\nflaky_port={flaky_port}\nflaky_port_2={flaky_port_2}\nslow_port={slow_port}\n"
        )
        sys.stdout.flush()
        sys.stdin.read()


if __name__ == "__main__":
    main()
