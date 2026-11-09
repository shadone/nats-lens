import type {Finding} from "../api/model";
import {InvestigationButton} from "./InvestigationContext";

// ASVS 1.2.3: observed text stays bounded data in the existing JSON annotation.
// Code-point limits keep UTF-8 below its 4000-byte import limit, including emoji.
const excerpt=(text:string,limit:number)=>[...text.replace(/\0/g,"\uFFFD")].slice(0,limit).join("");
export function FindingInvestigationButton({finding}:{finding:Finding}) {
  return <InvestigationButton label selection={{entity:finding.entity,check:finding.checkCode,evidence:[{kind:"finding",id:finding.id}],annotation:`Observed finding summary (UI evidence, may be shortened): ${excerpt(finding.checkName,300)}\n${excerpt(finding.detail,600)}`}} />;
}
