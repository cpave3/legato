// A select's options for things listed by name.
export function NameOptions({ items }: { items: { name: string }[] }) {
  return <>{items.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</>
}
