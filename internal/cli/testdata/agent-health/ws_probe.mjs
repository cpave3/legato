// Scratch credentials arrive over stdin and never appear in diagnostics.
import { createInterface } from 'node:readline';
const input = createInterface({ input: process.stdin });
const report = value => process.stdout.write(JSON.stringify(value) + '\n');
let socket;
const deadline = setTimeout(() => { console.error('scratch stream deadline'); process.exit(1); }, 120_000);
input.on('line', line => {
  const message = JSON.parse(line);
  if (message.close) { socket?.close(); return; }
  if (message.send) { socket.send(JSON.stringify(message.send)); return; }
  socket = new WebSocket(message.url);
  socket.onopen = () => {
    socket.send(JSON.stringify({type: 'subscribe_agent', agent_id: message.task, cursor: message.cursor ?? ''}));
    report({type: 'open'});
  };
  socket.onmessage = ({data}) => {
    const message = JSON.parse(data);
    if (message.type === 'error') report({type: 'error', error: message.error});
    if (message.type === 'chimera_sync_required') report({type: 'sync'});
    if (message.type === 'agent_message_sent') report({type: 'sent', requestId: message.request_id});
    if (message.type !== 'chimera_event') return;
    const event = JSON.parse(message.content);
    report({type: 'event', eventId: event.eventId, eventType: event.type, reason: event.reason,
      errorCanary: String(event.error ?? '').includes('CLAUDE-NONSECRET-API-ERROR-CANARY'),
      marker: typeof event.content === 'string' && event.content.startsWith('CLAUDE-') ? event.content.slice(0, 200) : undefined});
  };
  socket.onerror = () => report({type: 'transport-error'});
  socket.onclose = () => { clearTimeout(deadline); input.close(); };
});
input.on('close', () => { socket?.close(); clearTimeout(deadline); });
