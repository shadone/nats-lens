import {useSearchParams} from 'react-router';
import {TimelineFindingEpisodes} from '../components/TimelineFindingEpisodes';
import {findingHistoryRange} from '../lib/findingHistory';
import {usePageTitle} from '../lib/usePageTitle';
export function FindingHistory(){
 usePageTitle('Finding occurrence history');const [p]=useSearchParams();let selected:ReturnType<typeof findingHistoryRange>|undefined;
 try{selected=findingHistoryRange(p)}catch{/* Invalid scope cannot fall back to system-wide reads. */}
 return <><h1>Finding occurrence history</h1>{!selected?<p role="alert">This history link requires a valid fixed interval and supported resource scope.</p>:<><p>Separate retained occurrences overlapping the selected interval, including resolved findings. Historical state and latest retained state stay separate. Retention and missing transitions limit the evidence; this is not a continuous-capture guarantee.</p>{p.get('account')&&!selected.entity?.account&&<p>Account {p.get('account')} is navigation context and does not add an account filter to this history read.</p>}<TimelineFindingEpisodes showAllResultsLink={false} key={JSON.stringify(selected)} {...selected}/></>}</>;
}
