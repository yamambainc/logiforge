// スナップショットツリーのビュワー。中身は、この段の次の PR で作る（design/webui/first-release.md）。
export function TreeScreen({ repo }: { repo: string }) {
  return <main className="repos"><h1 className="page-title mono">{repo}</h1></main>;
}
