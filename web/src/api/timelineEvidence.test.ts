import {beforeEach,expect,it,vi} from 'vitest';
import {create} from '@bufbuild/protobuf';
import {ListEventSelectionResponseSchema,GetEventBatchResponseSchema} from '../gen/natslens/v1/events_pb';
import {selectedTimelineEvents,capturedSelectionPage} from './timelineEvidence';
import {lens} from './client';
vi.mock('./client',()=>({lens:{getEventBatch:vi.fn(),listEventSelection:vi.fn()}}));
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

it('requires complete membership and exact current-page acknowledgement before displaying shared selection rows',async()=>{
 const selection={version:1 as const,system:'shop',from:range.start,to:range.end,ids:['1','2','3'],eventReadId:'3',limits:[]};
 const good={selectedIds:selection.ids,eventReadId:3n,offset:1,pageSize:1,nextOffset:2,selectedTotalCount:3,totalCount:1,events:[{id:'2'}]};
 vi.mocked(lens.listEventSelection).mockResolvedValue(create(ListEventSelectionResponseSchema,good));expect((await capturedSelectionPage('shop',selection,1,1)).items[0].id).toBe('2');
 for(const bad of [{...good,events:[{id:'1'}]},{...good,selectedIds:[]},{...good,nextOffset:0},{...good,offset:0},{...good,pageSize:50},{...good,selectedTotalCount:2},{...good,totalCount:0},{...good,eventReadId:undefined}]){vi.mocked(lens.listEventSelection).mockResolvedValue(create(ListEventSelectionResponseSchema,bad));await expect(capturedSelectionPage('shop',selection,1,1)).rejects.toThrow()}
});
