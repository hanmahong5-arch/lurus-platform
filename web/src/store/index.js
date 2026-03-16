import { create } from 'zustand'
import { getMe } from '../api/account'
import { getWallet } from '../api/wallet'
import { listSubscriptions } from '../api/subscription'

export const useStore = create((set, get) => ({
  account: null,
  wallet: null,
  subscriptions: [],
  loading: false,

  async init() {
    if (get().loading) return
    set({ loading: true })
    try {
      const [accRes, walRes, subRes] = await Promise.all([
        getMe(),
        getWallet(),
        listSubscriptions(),
      ])
      set({
        account: accRes.data,
        wallet: walRes.data,
        subscriptions: subRes.data?.subscriptions ?? [],
      })
    } catch (_) {
      // Errors handled by axios interceptor (401 → redirect)
    } finally {
      set({ loading: false })
    }
  },

  async refreshWallet() {
    const res = await getWallet()
    set({ wallet: res.data })
  },

  async refreshSubscriptions() {
    const res = await listSubscriptions()
    set({ subscriptions: res.data?.subscriptions ?? [] })
  },
}))
