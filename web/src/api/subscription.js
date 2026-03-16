import client from './client'

export const listSubscriptions = () => client.get('/subscriptions')
export const getSubscription = (productId) => client.get(`/subscriptions/${productId}`)
export const checkout = (data) => client.post('/subscriptions/checkout', data)
export const cancelSubscription = (productId) => client.post(`/subscriptions/${productId}/cancel`)
export const listProducts = () => client.get('/products')
export const listPlans = (productId) => client.get(`/products/${productId}/plans`)
