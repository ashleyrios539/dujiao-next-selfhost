import { api } from './client'

export const productAPI = {
    list: (params?: any) => api.get('/public/products', { params }),
    detail: (slug: string) => api.get(`/public/products/${slug}`),
    pickStock: (slug: string, bin?: string) => api.get(`/public/products/${slug}/pick-stock`, { params: bin ? { bin } : undefined }),
    // 「当前所选」灰框同行动态库存：按国家/首位/种类三维实时计数（与后端发货 buildPickQuery 一致）。
    pickCount: (slug: string, params: { country?: string; bin?: string; card_type?: string }) =>
        api.get(`/public/products/${slug}/pick-count`, { params }),
}

export const postAPI = {
    list: (params?: any) => api.get('/public/posts', { params }),
    detail: (slug: string) => api.get(`/public/posts/${slug}`),
}

export const bannerAPI = {
    list: (params?: any) => api.get('/public/banners', { params }),
}

export const categoryAPI = {
    list: (params?: any) => api.get('/public/categories', { params }),
}

export const memberLevelAPI = {
    list: () => api.get('/public/member-levels'),
}
