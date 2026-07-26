import AsyncStorage from "@react-native-async-storage/async-storage"
import * as SecureStore from "expo-secure-store"
import type { LegatoConnection } from "./model"

const CONNECTIONS = "legato.mobile.connections.v1"
const tokenKey = (id: string) => `legato.mobile.token.${id}`

export async function loadConnections(): Promise<LegatoConnection[]> {
  const raw = await AsyncStorage.getItem(CONNECTIONS)
  if (!raw) return []
  try { return JSON.parse(raw) as LegatoConnection[] } catch { return [] }
}

export async function saveConnection(connection: LegatoConnection, token: string): Promise<LegatoConnection[]> {
  const current = await loadConnections()
  const existing = current.find((item) => item.baseUrl === connection.baseUrl)
  const saved = existing ? { ...connection, id: existing.id, createdAt: existing.createdAt } : connection
  await SecureStore.setItemAsync(tokenKey(saved.id), token)
  const next = [...current.filter((item) => item.id !== saved.id), saved]
  await AsyncStorage.setItem(CONNECTIONS, JSON.stringify(next))
  return next
}

export async function renameConnection(id: string, name: string): Promise<LegatoConnection[]> {
  const current = await loadConnections()
  const next = current.map((item) => item.id === id ? { ...item, name: name.trim() } : item)
  await AsyncStorage.setItem(CONNECTIONS, JSON.stringify(next))
  return next
}

export async function updateConnection(id: string, updates: Pick<LegatoConnection, "name" | "baseUrl">, token?: string): Promise<LegatoConnection[]> {
  const current = await loadConnections()
  if (current.some((item) => item.id !== id && item.baseUrl === updates.baseUrl)) {
    throw new Error("Another connection already uses this server URL")
  }
  const next = current.map((item) => item.id === id ? { ...item, ...updates } : item)
  if (token?.trim()) await SecureStore.setItemAsync(tokenKey(id), token.trim())
  await AsyncStorage.setItem(CONNECTIONS, JSON.stringify(next))
  return next
}

export async function removeConnection(id: string): Promise<LegatoConnection[]> {
  await SecureStore.deleteItemAsync(tokenKey(id))
  const next = (await loadConnections()).filter((item) => item.id !== id)
  await AsyncStorage.setItem(CONNECTIONS, JSON.stringify(next))
  return next
}

export const loadToken = (id: string) => SecureStore.getItemAsync(tokenKey(id))
