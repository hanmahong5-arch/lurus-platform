import client from './client'

export const getWallet = () => client.get('/wallet')
export const getTopupInfo = () => client.get('/wallet/topup/info')
export const createTopup = (data) => client.post('/wallet/topup', data)
export const listTransactions = (params) => client.get('/wallet/transactions', { params })
export const listOrders = (params) => client.get('/wallet/orders', { params })
export const getOrder = (orderNo) => client.get(`/wallet/orders/${orderNo}`)
export const redeem = (code) => client.post('/wallet/redeem', { code })
