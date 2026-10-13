#!/usr/bin/env node
// Opt-in acceptance driver: run the real adapter and SIGKILL only this helper at
// a selected command boundary. No command is generated or retried by the driver.
// --adapter /absolute/installed/main.mjs --config /private/host.json
// --cut-id <operation-id> --cut-at before-dispatch|upstream-ack|journal-ack
// Feed a normal v1 operation on stdin, then retry the identical line through a
// fresh, ordinary installed helper. Never point this at a non-disposable task.
import { dirname, resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';
const argv = process.argv.slice(2);
const option = name => { const i = argv.indexOf(name); return i < 0 ? undefined : argv[i+1]; };
const adapter = resolve(option('--adapter') ?? new URL('../adapter/main.mjs', import.meta.url).pathname);
const cutId = option('--cut-id'), cutAt = option('--cut-at');
if (!cutId || !['before-dispatch', 'upstream-ack', 'journal-ack'].includes(cutAt)) {
  throw new Error('Supply --cut-id and --cut-at before-dispatch|upstream-ack|journal-ack.');
}
const { run } = await import(pathToFileURL(adapter).href);
const { T3 } = await import(pathToFileURL(join(dirname(adapter), 't3.mjs')).href);
const die = () => process.kill(process.pid, 'SIGKILL');
await run(argv, { createBackend(config, emit) {
  const backend = new T3(config, emit), connect = backend.connect.bind(backend), act = backend.act.bind(backend);
  let wrapped;
  backend.connect = async () => {
    await connect();
    if (wrapped === backend.rpc) return;
    wrapped = backend.rpc;
    const call = backend.rpc.call.bind(backend.rpc);
    backend.rpc.call = (tag, payload, ...rest) => {
      const target = payload.commandId === cutId && ['orchestration.dispatchCommand', 'orchestration.launchThread'].includes(tag);
      if (target && cutAt === 'before-dispatch') die();
      const operation = call(tag, payload, ...rest);
      if (target && cutAt === 'upstream-ack') {
        operation.promise = operation.promise.then(result => { die(); return result; });
      }
      return operation;
    };
  };
  backend.act = async (op, a) => {
    const result = await act(op, a);
    if (a.id === cutId && cutAt === 'journal-ack') die();
    return result;
  };
  return backend;
} });
