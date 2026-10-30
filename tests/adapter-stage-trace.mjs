// Opt-in integration measurement entry. Imports the selected real adapter and
// wraps existing method boundaries without changing native requests or timers.
// Only fixed stage names, counts and monotonic milliseconds are retained.
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

export async function observeAdapter(sourceRoot, traceDirectory) {
  const origin = performance.now(), stages = [];
  const mark = (stage, extra = {}) => {
    if (stages.length < 512) stages.push({stage,atMs:performance.now()-origin,...extra});
  };
  mark('wrapper-entry');
  // PID is only a temporary filename discriminator. It is not copied into the
  // report; each result is assigned an opaque local helper ordinal by the test.
  process.on('exit', () => writeFileSync(join(traceDirectory, `${process.pid}.json`), JSON.stringify({
    schema:1,boundary:'Node wrapper-local monotonic clock; do not subtract from Go timestamps',stages,
  })));
  const [{run},{T3},{connectSocket}] = await Promise.all([
    import(pathToFileURL(join(sourceRoot,'adapter/main.mjs')).href),
    import(pathToFileURL(join(sourceRoot,'adapter/t3.mjs')).href),
    import(pathToFileURL(join(sourceRoot,'adapter/wire.mjs')).href),
  ]);
  mark('adapter-import-ready');
  let frame = 0, activeFrame = 0;
  const originalWrite = process.stdout.write.bind(process.stdout);
  process.stdout.write = (chunk, encoding, callback) => {
    if (typeof encoding === 'function') {callback=encoding;encoding=undefined;}
    const ordinal = activeFrame, bytes = Buffer.byteLength(chunk);
    mark('stdout-write-start',{frame:ordinal,bytes});
    return originalWrite(chunk,encoding,error=>{
      mark('stdout-write-callback',{frame:ordinal,bytes,failed:Boolean(error)});
      callback?.(error);
    });
  };
  await run(process.argv.slice(2), {
    createBackend(config, emit) {
      mark('private-config-validated');
      let requestSeen = false;
      const backend = new T3(config, (event, data) => {
        if (event === 'view' || event === 'delta') {
          activeFrame = ++frame;
          mark('companion-frame-ready',{frame,live:(data.metadata ?? data).live === true});
        } else activeFrame = 0;
        emit(event,data);
        activeFrame = 0;
      }, {connectSocket:async (...args) => {
        mark('socket-connect-start');
        const socket = await connectSocket(...args);
        mark('socket-connect-ready');
        const call = socket.call.bind(socket);
        socket.call = (tag,payload,...rest) => {
          if (tag === 'orchestration.subscribeThread') mark('subscription-call-admitted');
          return call(tag,payload,...rest);
        };
        return socket;
      }});
      const http = backend.http.bind(backend);
      backend.http = async (path,...args) => {
        const stage = path === '/.well-known/t3/environment' ? 'descriptor' : path === '/api/auth/websocket-ticket' ? 'ticket' : null;
        if (stage) mark(`${stage}-read-start`);
        const result = await http(path,...args);
        if (stage) mark(`${stage}-read-ready`);
        return result;
      };
      const probe = backend.probe.bind(backend);
      backend.probe = async (...args) => {
        mark('probe-start');
        const result = await probe(...args);
        mark('probe-verified');
        return result;
      };
      const bounded = backend.bounded.bind(backend);
      backend.bounded = async (...args) => {
        mark('bounded-read-start');
        const result = await bounded(...args);
        mark('bounded-read-validated');
        return result;
      };
      const event = backend.onEvent.bind(backend);
      backend.onEvent = item => {
        if (item.kind === 'synchronized') mark('subscription-marker-received');
        if (item.kind === 'event' && /^runtime-request\./u.test(item.event?.type)) {
          requestSeen = true;mark('native-request-event-received');
        }
        return event(item);
      };
      const schedule = backend.scheduleRefresh.bind(backend);
      backend.scheduleRefresh = (...args) => {
        if (requestSeen) mark('request-refresh-scheduled');
        return schedule(...args);
      };
      const refresh = backend.refresh.bind(backend);
      backend.refresh = async (...args) => {
        const request = requestSeen;
        if (request) mark('request-refresh-start');
        const result = await refresh(...args);
        if (request) {mark('request-refresh-complete');requestSeen=false;}
        return result;
      };
      return backend;
    },
  });
}
