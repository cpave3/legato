// Private scratch WebSocket client. Configuration arrives over stdin; never
// print the authenticated URL or event bodies (spawn events contain URLs).
import { createInterface } from 'node:readline';

const input = createInterface({ input: process.stdin });
let socket;
const deadline = setTimeout(() => {
  console.error('scratch stream deadline');
  process.exit(1);
}, 60_000);
const report = (value) => process.stdout.write(JSON.stringify(value) + '\n');
input.on('line', (line) => {
  const message = JSON.parse(line);
  if (message.close) {
    socket?.close();
    return;
  }
  socket = new WebSocket(message.url);
  socket.onopen = () => {
    socket.send(JSON.stringify({ type: 'subscribe_agent', agent_id: message.task, cursor: message.cursor ?? '' }));
    report({ type: 'open' });
  };
  socket.onmessage = ({ data }) => {
    const message = JSON.parse(data);
    if (message.type === 'chimera_sync_required') report({ type: 'sync' });
    if (message.type === 'error') report({ type: 'protocol-error' });
    if (message.type !== 'chimera_event') return;
    const event = JSON.parse(message.content);
    report({ type: 'event', eventId: event.eventId, eventType: event.type,
      append: event.append, subagentId: event.subagentId, parentCallId: event.parentCallId,
      childSessionId: event.childSessionId, reason: event.reason,
      recoveryWarning: event.warnings?.some(warning => warning.includes('lossy context recovery')),
      barrier: event.content === 'stream-ready-canary',
      reloadMessage: event.content === 'SCENARIO-MCP-RELOAD',
      nestedType: event.event?.type });
  };
  socket.onerror = () => report({ type: 'transport-error' });
  socket.onclose = () => { clearTimeout(deadline); input.close(); };
});
input.on('close', () => { socket?.close(); clearTimeout(deadline); });
