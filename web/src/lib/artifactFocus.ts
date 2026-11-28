// Which artifact the side panel shows, and which revision of it.
export interface ArtifactFocus {
  itemId: string
  // 0 follows the latest revision; a number pins one.
  revision: number
}
