import { expect, it, vi } from 'vitest';
import { createAPI } from './api';
import { readEventStream } from './task-stream.js';
function streamed(chunks){const encoder=new TextEncoder();return new Response(new ReadableStream({start(controller){for(const text of chunks)controller.enqueue(encoder.encode(text));controller.close();}}),{headers:{'Content-Type':'text/event-stream'}});}
it('handles CRLF boundaries split across network chunks',async()=>{
 const update=vi.fn();const ended=await readEventStream(streamed(['event: status\r','\ndata: {"message":"Writing"}\r','\n\r','\nevent: done\r\ndata: {"result":{}}\r','\n\r','\n']),update);
 expect(ended).toBe(true);expect(update.mock.calls.map(([e])=>e.event)).toEqual(['status','done']);
});
it('decodes split SSE frames, reports incremental updates, and returns only the final result',async()=>{
 const fetcher=vi.fn().mockResolvedValue(streamed(['event: status\ndata: {"message":"Writing code"}\n\nevent: del','ta\ndata: {"text":"print("}\n\nevent: done\ndata: {"status":200,"result":{"status":"planned"}}\n\n']));const update=vi.fn();const controller=new AbortController();
 const result=await createAPI({fetcher,getToken:()=> 'token'}).generatePlanStream('exp_test',update,controller.signal);expect(result).toEqual({status:'planned'});expect(update.mock.calls.map(([e])=>e.event)).toEqual(['status','delta','done']);expect(fetcher.mock.calls[0][0]).toBe('/api/v1/experiments/exp_test/plan/stream');expect(fetcher.mock.calls[0][1].headers.Authorization).toBe('Bearer token');expect(fetcher.mock.calls[0][1].signal).toBe(controller.signal);
});
it('does not report success when the stream disconnects or emits a validation error',async()=>{
 const broken=createAPI({fetcher:vi.fn().mockResolvedValue(streamed(['event: delta\ndata: {"text":"partial"}\n\n']))});await expect(broken.chatStream({messages:[]},vi.fn())).rejects.toMatchObject({code:'stream_interrupted'});
 const invalid=createAPI({fetcher:vi.fn().mockResolvedValue(streamed(['event: error\ndata: {"status":422,"error":{"code":"invalid_plan","message":"Review this method"}}\n\n']))});await expect(invalid.generatePlanStream('exp_test',vi.fn())).rejects.toMatchObject({code:'invalid_plan',message:'Review this method',status:422});
});
