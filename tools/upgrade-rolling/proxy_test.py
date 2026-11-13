import socket
import socketserver
import threading
import unittest
from proxy import Passthrough


class ProxyTest(unittest.TestCase):
    def test_opaque_bytes_route_to_both_backends_and_replacement(self):
        class Echo(socketserver.BaseRequestHandler):
            def handle(self):
                data = self.request.recv(4096)
                self.request.sendall(self.server.label + data)

        servers, threads = [], []
        proxy = None
        try:
            for label in (b"old-0:", b"old-1:", b"new-0:"):
                server = socketserver.ThreadingTCPServer(("127.0.0.1", 0), Echo)
                server.daemon_threads = True
                server.label = label
                thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
                thread.start()
                servers.append(server)
                threads.append(thread)
            proxy = Passthrough([("old-0", servers[0].server_address), ("old-1", servers[1].server_address)])
            payload = b"\x00\xffopaque-tls-fixture\x00"
            def exchange():
                with socket.create_connection(proxy.address, timeout=2) as client:
                    client.sendall(payload)
                    return client.recv(4096)
            self.assertEqual(exchange(), b"old-0:" + payload)
            self.assertEqual(exchange(), b"old-1:" + payload)
            proxy.set_backends([("new-0", servers[2].server_address)])
            self.assertEqual(exchange(), b"new-0:" + payload)
            self.assertEqual(proxy.counts(), {"old-0": 1, "old-1": 1, "new-0": 1})
            for invalid in ([], [("public", ("example.com", 443))], [("duplicate", servers[0].server_address)] * 2):
                with self.assertRaises(ValueError):
                    proxy.set_backends(invalid)
        finally:
            if proxy:
                proxy.close()
            for server in servers:
                server.shutdown()
                server.server_close()
            for thread in threads:
                thread.join(timeout=2)


if __name__ == "__main__":
    unittest.main()
