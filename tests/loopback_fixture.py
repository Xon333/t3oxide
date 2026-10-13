#!/usr/bin/env python3
"""Optional development fixture using aiohttp. No real T3 server or agent is touched.
Exercises actual Node HTTP/WebSocket and stdio, not a provider or native UI.
The cloud pass has aiohttp available; native installers need not run this again.
"""
import asyncio
import copy
import hashlib
import json
import os
from pathlib import Path
import tempfile
from aiohttp import web, WSMsgType

ROOT = Path(__file__).resolve().parents[1]
TOKEN = 'fixture-token-not-a-credential'


def row(name, text):
    return {'sourceThreadId': 'thread-a', 'sourceItemId': name, 'visibility': 'local',
            'item': {'id': name, 'threadId': 'thread-a', 'type': 'assistant_message',
                     'text': text, 'messageId': 'msg-' + name, 'streaming': False}}


async def main():
    state = {'sequence': 10, 'effects': 0, 'held': False, 'receipts': {}, 'sockets': {}, 'acks': 0, 'commands': {}, 'dispatches': 0}
    question = {'id': 'approval', 'threadId': 'thread-a', 'type': 'approval_request',
                'requestId': 'request-a', 'requestKind': 'command', 'prompt': 'Approve fixture?',
                'options': [{'decision': 'accept', 'label': 'Once'}, {'decision': 'decline', 'label': 'Decline'}]}
    projection = {'thread': {'id': 'thread-a', 'title': 'Protocol fixture', 'projectId': 'project-a',
                            'modelSelection': {'instanceId': 'fixture-provider', 'model': 'fixture-model'},
                            'runtimeMode': 'approval-required', 'interactionMode': 'default', 'worktreePath': None},
                  'runs': [{'id': 'run-a', 'status': 'running'}], 'visibleTurnItems': [row('first', 'Fixture response')],
                  'turnItems': [question], 'runtimeRequests': [{'id': 'request-a', 'status': 'pending', 'responseCapability': {'type': 'live', 'providerSessionId': 'p'}}]}
    descriptor = {'environmentId': 'env-a', 'orchestrationProtocolVersion': 2, 'serverVersion': '0.0.0-fixture',
                  'capabilities': {'serverResolvedCommandContext': True}}

    async def broadcast(kind, payload):
        state['sequence'] += 1
        for socket, subscription in list(state['sockets'].items()):
            if subscription and not socket.closed:
                await socket.send_json({'_tag': 'Chunk', 'requestId': subscription, 'values': [
                    {'kind': 'event', 'sequence': state['sequence'],
                     'event': {'threadId': 'thread-a', 'type': kind, 'payload': payload}}]})

    async def handler(request):
        if request.path == '/.well-known/t3/environment':
            return web.json_response(descriptor)
        if request.path == '/ws':
            assert request.query.get('wsTicket') == 'fixture-ticket'
            assert request.query.get('orchestrationProtocol') == '2'
            socket = web.WebSocketResponse()
            await socket.prepare(request)
            state['sockets'][socket] = None
            try:
                async for message in socket:
                    if message.type != WSMsgType.TEXT:
                        continue
                    data = json.loads(message.data)
                    if data['_tag'] == 'Ack':
                        state['acks'] += 1
                        continue
                    if data['_tag'] == 'Interrupt':
                        state['sockets'][socket] = None
                        continue
                    if data['_tag'] == 'Ping':
                        await socket.send_json({'_tag': 'Pong'})
                        continue
                    assert data['_tag'] == 'Request'
                    tag, payload, rid = data['tag'], data['payload'], data['id']
                    if tag == 'orchestration.subscribeThread':
                        assert payload['acceptBoundedSnapshot'] is True
                        assert payload['requestCompletionMarker'] is True
                        state['sockets'][socket] = rid
                        await socket.send_json([{'_tag': 'Chunk', 'requestId': rid, 'values': [{'kind': 'synchronized'}]}])
                        continue
                    assert tag == 'orchestration.dispatchCommand'
                    cid = payload['commandId']
                    state['dispatches'] += 1
                    if cid in state['commands']:
                        assert state['commands'][cid] == payload, 'Retry changed the original upstream payload'
                    state['commands'][cid] = copy.deepcopy(payload)
                    if cid not in state['receipts']:
                        state['effects'] += 1
                        if payload['type'] == 'message.dispatch':
                            assert payload['createdBy'] == 'user' and payload['creationSource'] == 'web'
                            assert payload['deliveryIntent'] == 'auto'
                            projection['visibleTurnItems'].append(row('reply', 'Response after send'))
                            await broadcast('turn-item.updated', projection['visibleTurnItems'][-1]['item'])
                        elif payload['type'] == 'runtime-request.respond':
                            if 'decision' in payload:
                                assert payload['decision'] == 'accept'
                            else:
                                assert payload['answers'] == {'fixture': 'Continue'}
                            assert payload['requestId'] == projection['runtimeRequests'][0]['id']
                            projection['runtimeRequests'][0]['status'] = 'resolved'
                            await broadcast('runtime-request.updated', projection['runtimeRequests'][0])
                        elif payload['type'] == 'run.interrupt':
                            assert payload['runId'] == projection['runs'][0]['id']
                            state['held'] = payload.get('holdQueue') is True
                            projection['runs'][0]['status'] = 'interrupted'
                            await broadcast('run.updated', projection['runs'][0])
                        else:
                            raise AssertionError(payload['type'])
                        state['receipts'][cid] = {'sequence': state['sequence']}
                    if state.get('drop_ack_for') == cid:
                        continue
                    await socket.send_json({'_tag': 'Exit', 'requestId': rid, 'exit': {'_tag': 'Success', 'value': state['receipts'][cid]}})
            finally:
                state['sockets'].pop(socket, None)
            return socket
        assert request.headers.get('Authorization') == 'Bearer ' + TOKEN
        assert request.headers.get('x-t3-orchestration-protocol') == '2'
        if request.path == '/api/auth/websocket-ticket':
            return web.json_response({'ticket': 'fixture-ticket'})
        if request.path == '/api/orchestration/shell':
            return web.json_response({'threads': [dict(projection['thread'], status='running')],
                                      'projects': [{'id': 'project-a', 'title': 'Fixture project'}]})
        if request.path.endswith('/bounded'):
            return web.json_response({'snapshotSequence': state['sequence'], 'projection': copy.deepcopy(projection),
                                      'historyCursor': 'opaque-fixture', 'hasMoreHistory': True})
        if request.path.endswith('/history'):
            assert request.query.get('cursor') == 'opaque-fixture'
            return web.json_response({'items': [row('old', 'Older page')], 'nextCursor': None, 'hasMoreHistory': False})
        raise web.HTTPNotFound()

    app = web.Application()
    app.router.add_route('*', '/{path:.*}', handler)
    runner = web.AppRunner(app)
    await runner.setup()
    site = web.TCPSite(runner, '127.0.0.1', 0)
    await site.start()
    port = site._server.sockets[0].getsockname()[1]
    with tempfile.TemporaryDirectory(prefix='t3oxide-fixture-') as temp:
        token = Path(temp) / 'token'
        token.write_text(TOKEN)
        token.chmod(0o600)
        config = Path(temp) / 'host.json'
        config.write_text(json.dumps({'baseUrl': f'http://127.0.0.1:{port}', 'environmentId': 'env-a', 'tokenFile': str(token), 'allowWrites': True}))
        config.chmod(0o600)
        captured = []
        current = None
        deltas = []
        async def launch(cut_id=None, cut_at=None):
            args = ['node', str(ROOT / 'tests/recovery-driver.mjs'), '--cut-id', cut_id, '--cut-at', cut_at] if cut_id else ['node', str(ROOT / 'adapter/main.mjs')]
            return await asyncio.create_subprocess_exec(*args, '--config', str(config),
                stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
        process = await launch()
        async def until(predicate):
            async def read():
                nonlocal current
                while True:
                    line = await process.stdout.readline()
                    if not line:
                        raise AssertionError((await process.stderr.read()).decode())
                    captured.append(line)
                    packet = json.loads(line)
                    if packet.get('event') == 'view':
                        current = packet['data']
                        assert isinstance(current['revision'], int)
                    elif packet.get('event') == 'delta':
                        delta = packet['data']
                        assert current is not None and delta['threadId'] == current['threadId']
                        assert delta['baseRevision'] == current['revision']
                        assert delta['revision'] > delta['baseRevision']
                        rows = {item['key']: item for item in current['items']}
                        for key in delta['removed']:
                            rows.pop(key, None)
                        rows.update({item['key']: item for item in delta['upserts']})
                        assert set(rows) == set(delta['order'])
                        assert len(delta['order']) == len(set(delta['order']))
                        current = dict(delta['metadata'], revision=delta['revision'],
                                       items=[rows[key] for key in delta['order']])
                        deltas.append(delta)
                    if predicate(packet):
                        return packet
            return await asyncio.wait_for(read(), 8)
        async def until_view(predicate):
            if current is None or not predicate(current):
                await until(lambda packet: packet.get('event') in ('view', 'delta') and predicate(current))
            return current
        async def request(op, **fields):
            rid = fields.pop('id', op + '-request')
            process.stdin.write((json.dumps(dict(v=1, id=rid, op=op, environmentId='env-a', **fields)) + '\n').encode())
            await process.stdin.drain()
            response = await until(lambda packet: packet.get('id') == rid)
            assert 'error' not in response, response
            return response.get('result', {})
        async def close():
            process.stdin.close()
            await asyncio.wait_for(process.wait(), 4)
            assert process.returncode == 0
        try:
            assert (await request('hello'))['environmentId'] == 'env-a'
            assert (await request('list'))['threads'][0]['id'] == 'thread-a'
            await request('watch', threadId='thread-a')
            initial = await until_view(lambda view: view.get('live'))
            assert initial['state'] == 'live'
            approval = initial['requests'][0]
            projection['visibleTurnItems'][0]['item']['text'] = 'One incremental change'
            await broadcast('turn-item.updated', projection['visibleTurnItems'][0]['item'])
            await until_view(lambda view: view['items'][0]['text'] == 'One incremental change')
            assert len(deltas[-1]['upserts']) == 1
            await request('send', id='persistent-send', threadId='thread-a', message='Task', mode='auto')
            sent = await until_view(lambda view: any(i['itemId'] == 'reply' for i in view['items']))
            assert sent['items'][-1]['text'] == 'Response after send'
            await request('answer', threadId='thread-a', requestId=approval['id'], revision=approval['revision'], decision='accept')
            await request('stop', threadId='thread-a', runId='run-a')
            assert state['held'] and state['effects'] == 3
            await request('history', cursor=current['historyCursor'])
            historical = await until_view(lambda view: view.get('historical') and view['items'][0]['itemId'] == 'old')
            assert historical['items'][0]['text'] == 'Older page'
            assert historical['state'] == 'historical' and not historical['live']
            await close()
            process = await launch()
            await request('send', id='persistent-send', threadId='thread-a', message='Task', mode='auto')
            assert state['effects'] == 3, 'Restart retry duplicated a durable command'
            await close()
            # Kill the actual helper at each side of the network/durable ACK
            # boundaries. Each retry uses an unchanged legacy v1 request and a
            # fresh helper, after the applied control has become stale.
            for op in ('stop', 'approval', 'question'):
                for cut in ('before-dispatch', 'before-ack', 'upstream-ack', 'journal-ack'):
                    cid = f'crash-{op}-{cut}'
                    projection['runs'] = [{'id': cid + '-run', 'status': 'running'}]
                    request_row = {'id': cid + '-request', 'status': 'pending',
                                   'responseCapability': {'type': 'live', 'providerSessionId': 'fixture'}}
                    item = dict(question, requestId=request_row['id']) if op == 'approval' else {
                        'id': cid + '-item', 'type': 'user_input_request', 'requestId': request_row['id'],
                        'questions': [{'id': 'fixture', 'header': 'Fixture', 'required': True}]}
                    projection['runtimeRequests'] = [request_row]
                    projection['turnItems'] = [item]
                    revision = hashlib.sha256(json.dumps({'request': request_row, 'item': item},
                        sort_keys=True, separators=(',', ':')).encode()).hexdigest()
                    fields = {'threadId': 'thread-a'}
                    if op == 'stop':
                        fields['runId'] = projection['runs'][0]['id']
                    else:
                        fields.update(requestId=request_row['id'], revision=revision)
                        fields.update(decision='accept') if op == 'approval' else fields.update(answers={'fixture': 'Continue'})
                    actual_op = 'stop' if op == 'stop' else 'answer'
                    action = dict(v=1, id=cid, op=actual_op, environmentId='env-a', **fields)
                    effects_before = state['effects']
                    state['drop_ack_for'] = cid if cut == 'before-ack' else None
                    process = await launch() if cut == 'before-ack' else await launch(cid, cut)
                    process.stdin.write((json.dumps(action) + '\n').encode())
                    await process.stdin.drain()
                    if cut == 'before-ack':
                        async def applied():
                            while cid not in state['receipts']:
                                await asyncio.sleep(0.01)
                        await asyncio.wait_for(applied(), 8)
                        process.kill()
                    await asyncio.wait_for(process.wait(), 8)
                    state['drop_ack_for'] = None
                    assert process.returncode == -9, (cut, (await process.stderr.read()).decode())
                    captured.append(await process.stdout.read())
                    assert state['effects'] == effects_before + (cut != 'before-dispatch')
                    # An accepted Stop must not affect a different, newer run;
                    # an accepted answer must not answer a new pending request.
                    if cut != 'before-dispatch':
                        projection['runs'] = [{'id': 'later-run', 'status': 'running'}]
                        projection['runtimeRequests'] = [{'id': 'later-request', 'status': 'pending',
                            'responseCapability': {'type': 'live', 'providerSessionId': 'fixture'}}]
                    dispatches_before = state['dispatches']
                    process = await launch()
                    result = await request(actual_op, id=cid, **fields)
                    assert isinstance(result['sequence'], int)
                    assert state['effects'] == effects_before + 1
                    assert state['dispatches'] == dispatches_before + (cut != 'journal-ack')
                    if cut != 'before-dispatch':
                        assert projection['runs'][0]['status'] == 'running'
                        assert projection['runtimeRequests'][0]['status'] == 'pending'
                    # Conflicting same-ID retries must fail even in a new helper.
                    await close()
                    process = await launch()
                    conflict = dict(action, threadId='different-thread')
                    process.stdin.write((json.dumps(conflict) + '\n').encode())
                    await process.stdin.drain()
                    rejected = await until(lambda packet: packet.get('id') == cid)
                    assert rejected['error']['code'] == 'invalid_request'
                    assert state['effects'] == effects_before + 1
                    await close()
            assert state['acks'] > 0
            assert TOKEN.encode() not in b''.join(captured)
            print('Loopback fixture passed: real HTTP/WS/stdio plus SIGKILL before dispatch, before upstream ACK, after upstream ACK, and after durable ACK for Stop/approval/question; exact fresh-helper retries, conflict rejection, no new-run/request effect, no token output.')
        finally:
            if process.returncode is None:
                process.kill()
                await process.wait()
            await runner.cleanup()


if __name__ == '__main__':
    asyncio.run(main())
