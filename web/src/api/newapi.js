import axios from 'axios'
import { getToken } from '../auth'

const newapi = axios.create({
  baseURL: '/proxy/newapi/api',
  timeout: 30000,
})

// Attach Zitadel admin JWT — the backend proxy replaces it with a newapi token.
newapi.interceptors.request.use((config) => {
  const token = getToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// NewAPI returns { success: false, message: "..." } for business errors (HTTP 200).
newapi.interceptors.response.use(
  (res) => {
    if (res.data?.success === false) {
      const err = new Error(res.data.message || 'Operation failed')
      err.response = res
      throw err
    }
    return res
  },
  (err) => {
    if (err.response?.status === 502) {
      throw new Error('LLM 网关不可达')
    }
    return Promise.reject(err)
  }
)

export default newapi
