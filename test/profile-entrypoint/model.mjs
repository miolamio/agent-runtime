// Synthetic Anthropic-compatible responses, restricted to the shared loopback
// namespace of two --network none containers. No real credentials or API calls.
import http from 'node:http';
import * as fs from 'node:fs/promises';
const server = http.createServer(async (request, response) => {
  try {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    if (request.url.includes('count_tokens')) { response.setHeader('content-type', 'application/json'); response.end('{"input_tokens":100}'); return; }
    if (!request.url.startsWith('/v1/messages')) { response.setHeader('content-type', 'application/json'); response.end('{}'); return; }
    const body = JSON.parse(raw);
    await fs.appendFile('/proof/requests.jsonl', JSON.stringify(body) + '\n');
    const message = { id: 'msg_entrypoint', type: 'message', role: 'assistant', model: body.model,
      content: [{ type: 'text', text: 'AIRUN_ENTRYPOINT_MODEL_COMPLETE' }], stop_reason: 'end_turn', stop_sequence: null,
      usage: { input_tokens: 100, output_tokens: 10 } };
    if (!body.stream) { response.setHeader('content-type', 'application/json'); response.end(JSON.stringify(message)); return; }
    response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' });
    const event = (type, data) => response.write(`event: ${type}\ndata: ${JSON.stringify({ type, ...data })}\n\n`);
    event('message_start', { message: { ...message, content: [], stop_reason: null, usage: { input_tokens: 100, output_tokens: 0 } } });
    event('content_block_start', { index: 0, content_block: { type: 'text', text: '' } });
    event('content_block_delta', { index: 0, delta: { type: 'text_delta', text: message.content[0].text } });
    event('content_block_stop', { index: 0 });
    event('message_delta', { delta: { stop_reason: 'end_turn', stop_sequence: null }, usage: { output_tokens: 10 } });
    event('message_stop', {});
    response.end();
  } catch (error) { response.statusCode = 500; response.end(error.message); }
});
await new Promise(resolve => server.listen(8383, '127.0.0.1', resolve));
await fs.writeFile('/proof/server-ready', 'ready');
