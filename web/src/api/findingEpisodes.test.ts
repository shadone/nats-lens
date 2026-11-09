import {create} from '@bufbuild/protobuf';
import {timestampFromMs} from '@bufbuild/protobuf/wkt';
import {expect,it,vi} from 'vitest';
import {lens} from './client';
import {listFindingEpisodes,epoch} from './findingEpisodes';
import {ListFindingEpisodesResponseSchema} from '../gen/natslens/v1/finding_episodes_pb';
import {FindingState} from '../gen/natslens/v1/findings_pb';
vi.mock('./client',()=>({lens:{listFindingEpisodes:vi.fn()}}));
it('preserves exact epoch identity, known zero boundaries and separate historical/latest states',async()=>{
 vi.mocked(lens.listFindingEpisodes).mockResolvedValue(create(ListFindingEpisodesResponseSchema,{readEpoch:9007199254740995n,totalCount:1,episodes:[{firstRetainedEpoch:9007199254740993n,startEpoch:9007199254740993n,finding:{id:'x',state:FindingState.FIRING},latestRetainedState:FindingState.RESOLVED,firstRetainedAt:timestampFromMs(1000),transitions:[{epoch:9007199254740993n}]}]}));
 const signal=new AbortController().signal;
 const r=await listFindingEpisodes('shop',1000,2000,{readEpoch:'0',firstEpoch:'9007199254740993',findingId:'x',includeTransitions:true},signal);
 expect(lens.listFindingEpisodes).toHaveBeenCalledWith(expect.objectContaining({system:'shop',readEpoch:0n,firstRetainedEpoch:9007199254740993n,includeTransitions:true}),{signal});
 expect(r.readEpoch).toBe('9007199254740995');expect(r.items[0]).toMatchObject({firstEpoch:'9007199254740993',finding:{state:'firing'},latestState:'resolved',transitions:[{epoch:'9007199254740993'}]});
});
it('rejects malformed and overflow collection identities rather than rounding them',()=>{
 for(const x of ['-1','01','1.5','9223372036854775808','1e3',''])expect(()=>epoch(x)).toThrow();
 expect(epoch('9223372036854775807')).toBe(9223372036854775807n);
});
it('rejects a source that ignores the captured occurrence boundary, including known empty capture',async()=>{
 for(const [wanted,returned] of [['0',5n],['5',6n]]){
 vi.mocked(lens.listFindingEpisodes).mockResolvedValue(create(ListFindingEpisodesResponseSchema,{readEpoch:returned}));await expect(listFindingEpisodes('shop',1000,2000,{readEpoch:wanted})).rejects.toThrow(/boundary/i);
 }
});
