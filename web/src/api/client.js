import axios from 'axios'
import { getToken, login } from '../auth'

const client = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

// Attach JWT (lurus session token preferred over Zitadel token)
client.interceptors.request.use((config) => {
  const token = getToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// On 401: redirect to the login page so the user can choose their login method.
client.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      const path = window.location.pathname
      // Avoid redirect loops on auth-related pages.
      if (!path.startsWith('/callback') && !path.startsWith('/login') && !path.startsWith('/zlogin')) {
        sessionStorage.setItem('login_return', path || '/wallet')
        window.location.href = '/login'
      }
    }
    return Promise.reject(err)
  }
)

export default client
