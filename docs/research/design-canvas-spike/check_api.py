"""Bounded proof through HTTP; no production imports or Legato state."""
import json
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
from pathlib import Path
from host import create_server


class FeedbackRoundTrip(unittest.TestCase):
    def test_feedback_survives_host_restart_and_stale_submission_is_rejected(self):
        with tempfile.TemporaryDirectory(prefix='PROTOTYPE-design-') as folder:
            def start():
                server = create_server(Path(folder) / 'PROTOTYPE-state.json', 0)
                thread = threading.Thread(target=server.serve_forever, daemon=True)
                thread.start()
                return server, thread, f'http://127.0.0.1:{server.server_port}'

            def call(path, body=None):
                request = urllib.request.Request(base + path, data=None if body is None else json.dumps(body).encode(), headers={'Content-Type': 'application/json'})
                with urllib.request.urlopen(request) as response:
                    return json.load(response)

            server, thread, base = start()
            try:
                document = call('/api/document')
                pin = {'document': document['id'], 'revision': document['revision'], 'frame': 'overview', 'anchor': 'primary-action', 'comment': 'Name the action more clearly'}
                saved = call('/api/feedback', pin)
                self.assertEqual(call('/api/feedback')[0], saved)
            finally:
                server.shutdown()
                server.server_close()
                thread.join()
            server, thread, base = start()
            try:
                self.assertEqual(call('/api/feedback')[0], saved)
                next_document = call('/api/advance', {'expected_revision': document['revision']})
                self.assertNotEqual(next_document['revision'], document['revision'])
                with self.assertRaises(urllib.error.HTTPError) as stale:
                    call('/api/feedback', pin)
                self.assertEqual(stale.exception.code, 409)
                self.assertEqual(call('/api/feedback')[0]['revision'], document['revision'])
                old = call('/api/document?revision=' + document['revision'])
                self.assertEqual(old, document)
            finally:
                server.shutdown()
                server.server_close()
                thread.join()


if __name__ == '__main__':
    unittest.main()
