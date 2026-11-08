export interface AggregateMetric {key:string;title:string;metrics:string[];labels:string[];explanation:string;inventory:string;inventoryLabel:string;inventoryNote:string;events:boolean}
export const aggregateMetrics:AggregateMetric[]=[];
export function aggregateMetric(_key:string):AggregateMetric|undefined{return undefined}
export function aggregateDestination(_metric:string):string|undefined{return undefined}
