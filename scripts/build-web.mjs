// Baseline adapter: production web currently rebuilds on every make invocation.
import {join} from 'node:path';
export async function buildWeb({root,build}) {
  await build(join(root,'internal/webui/dist'));
  return {built:true};
}
