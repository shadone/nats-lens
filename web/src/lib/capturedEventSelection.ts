import {eventReadId,frozenEventRange} from '../api/eventIdentity';
import type {TimelineCaptureLimit} from '../components/TimelineCaptureNotice';
export interface CapturedSelection {version:1;system:string;from:string;to:string;ids:string[];eventReadId?:string;limits:TimelineCaptureLimit[]}
export function selectionPath(_s:CapturedSelection,_offset=0,_pageSize=50):string{return '/timeline/events'}
export function selectionFromURL(_u:URL):{selection:CapturedSelection;offset:number;pageSize:number}{throw new Error('Captured selection is unavailable')}
export function validateSelection(s:CapturedSelection){
 frozenEventRange({start:s.from,end:s.to});eventReadId(s.eventReadId);
}
