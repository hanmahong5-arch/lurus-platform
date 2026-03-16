import client from './client'

export const getMe = () => client.get('/account/me')
export const updateMe = (data) => client.put('/account/me', data)
export const getServices = () => client.get('/account/me/services')
