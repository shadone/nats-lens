import {beforeEach,expect,it,vi} from 'vitest';
import {create} from '@bufbuild/protobuf';
import {GetEventBatchResponseSchema} from '../gen/natslens/v1/events_pb';
import {selectedTimelineEvents} from './timelineEvidence';
import {lens} from './client';
vi.mock('./client',()=>({lens:{getEventBatch:vi.fn()}}));
const range={start:'2026-10-17T13:00:00.000123Z',end:'2026-10-17T14:00:00.000456Z'};
beforeEach(()=>vi.clearAllMocks());
it('reads only exact IDs without excluding supporting observations outside the Timeline range',async()=>{
 vi.mocked(lens.getEventBatch).mockResolvedValue(create(GetEventBatchResponseSchema,{selectedIds:['9007199254740993'],eventReadId:9007199254740995n,events:[{id:'9007199254740993'}],totalCount:1}));
 const r=await selectedTimelineEvents('shop',range,['9007199254740993'],'9007199254740995');
 expect(lens.getEventBatch).toHaveBeenCalledWith({system:'shop',ids:['9007199254740993'],eventReadId:9007199254740995n},{signal:undefined});expect(r.items[0].id).toBe('9007199254740993');
});
it('rejects missing acknowledgement, broader rows and oversized input before display',async()=>{
 for(const response of [{events:[{id:'2'}],totalCount:1},{selectedIds:['1'],events:[{id:'2'}],totalCount:1},{selectedIds:['1'],eventReadId:2n,events:[],totalCount:0}]){
 vi.mocked(lens.getEventBatch).mockResolvedValue(create(GetEventBatchResponseSchema,response));await expect(selectedTimelineEvents('shop',range,['1'],'3')).rejects.toThrow();
 }
 vi.clearAllMocks();await expect(selectedTimelineEvents('shop',range,Array.from({length:51},(_,i)=>`${i+1}`),'3')).rejects.toThrow();expect(lens.getEventBatch).not.toHaveBeenCalled();
});
it('retains known missing rows rather than substituting nearby evidence',async()=>{
 vi.mocked(lens.getEventBatch).mockResolvedValue(create(GetEventBatchResponseSchema,{selectedIds:['1'],eventReadId:0n,totalCount:0}));expect((await selectedTimelineEvents('shop',range,['1'],'0')).items).toEqual([]);
});
