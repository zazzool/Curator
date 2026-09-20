import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { go, readRoute, routePath, sectionOf, sectionPath, useRoute } from './router'
import { SECTIONS } from './sections'

describe('адреса студии', () => {
  it('корень — это список задач', () => {
    // Первая строка колонки и то место, где проводят рабочий день.
    // Открывайся студия на источниках — первая строка меню была бы одним,
    // а открывалось бы другое.
    expect(readRoute('/')).toEqual({ name: 'cases', query: {} })
    expect(readRoute('')).toEqual({ name: 'cases', query: {} })
  })

  it('отбор списка задач разбирается из хвоста адреса', () => {
    expect(readRoute('/cases?source=2&status=draft&q=сроки')).toEqual({
      name: 'cases',
      query: { source: 2, status: 'draft', q: 'сроки' },
    })
  })

  it('пустые доводы отбора в адрес не пишутся', () => {
    // `/cases?source=&status=` и `/cases` — один и тот же отбор, но
    // адреса разные, и сравнение «мы уже здесь» различило бы их.
    expect(routePath({ name: 'cases', query: { path: '', q: '' } })).toBe('/cases')
    expect(readRoute('/cases?source=0')).toEqual({ name: 'cases', query: {} })
  })

  it('задача опознаётся строкой, а не числом', () => {
    // Опознаватель выдаёт сервер, и вида его студия не знает.
    expect(readRoute('/cases/c-abcdefgh23456789')).toEqual({
      name: 'case',
      id: 'c-abcdefgh23456789',
    })
  })

  it('источник опознаётся номером', () => {
    expect(readRoute('/sources/12')).toEqual({ name: 'source', id: 12 })
  })

  it('отбор в хвосте адреса страницу не меняет', () => {
    // Хвост после «?» разбирает сама страница: смешай его с выбором
    // страницы — и отбор списка уводил бы на ненайденный адрес.
    expect(readRoute('/sources?status=draft')).toEqual({ name: 'sources' })
    expect(readRoute('/sources/12#куски')).toEqual({ name: 'source', id: 12 })
  })

  it('нечисло вместо номера — ненайденный адрес, а не источник', () => {
    // Иначе опечатка доехала бы до сервера запросом источника «NaN», и
    // объяснять её пришлось бы отказом сервера вместо внятных слов.
    expect(readRoute('/sources/двенадцать')).toEqual({
      name: 'unknown',
      path: '/sources/двенадцать',
    })
    expect(readRoute('/sources/0')).toEqual({ name: 'unknown', path: '/sources/0' })
  })

  it('чужой адрес не подменяется списком источников', () => {
    // Сервер отдаёт студию на любой путь — иначе прямая ссылка на
    // источник не открылась бы. Показать на опечатку список источников
    // значит соврать: человек решит, что попал куда хотел.
    expect(readRoute('/истопники')).toEqual({ name: 'unknown', path: '/истопники' })
  })

  it('у каждого раздела колонки есть свой адрес, и он разбирается обратно', () => {
    // Колонка ведёт по адресам, и раздел, которого не знает разбор, увёл
    // бы составителя на «такой страницы нет» — по собственной строке меню.
    for (const one of SECTIONS) {
      const route = readRoute(sectionPath(one.id))
      expect(sectionOf(route)).toBe(one.id)
    }
  })

  it('адрес складывается обратно из разобранного', () => {
    for (const path of [
      '/sources',
      '/sources/12',
      '/cases',
      '/cases/c-abcdefgh23456789',
      '/generate',
      '/generate?source=2&unit=3.1',
      '/packs',
      '/sales',
      '/audiences',
      '/reports',
      '/workshop',
    ]) {
      expect(routePath(readRoute(path))).toBe(path)
    }
  })

  it('у ненайденного адреса раздела нет', () => {
    // Подсветить «Источники» на ненайденном адресе значит сказать, что мы
    // в Источниках.
    expect(sectionOf({ name: 'unknown', path: '/истопники' })).toBeNull()
  })
})

describe('переходы по студии', () => {
  beforeEach(() => {
    window.history.replaceState(null, '', '/sources')
  })

  it('свой переход перерисовывает экран', async () => {
    // `history.pushState` НЕ будит `popstate` — это записано в стандарте,
    // и на это наступают все, кто пишет маршрутизацию руками: адрес
    // меняется, а экран остаётся прежним. Проверка стоит ровно здесь.
    const { result } = renderHook(() => useRoute())
    expect(result.current).toEqual({ name: 'sources' })

    act(() => go({ name: 'source', id: 12 }))

    expect(window.location.pathname).toBe('/sources/12')
    expect(result.current).toEqual({ name: 'source', id: 12 })
  })

  it('«назад» браузера возвращает на прежнюю страницу студии', async () => {
    // Без этого «назад» выкидывало из студии целиком — на страницу, с
    // которой в неё вошли.
    const { result } = renderHook(() => useRoute())
    act(() => go({ name: 'source', id: 12 }))
    act(() => go({ name: 'packs' }))

    act(() => window.history.back())

    await waitFor(() => expect(result.current).toEqual({ name: 'source', id: 12 }))
  })

  it('переход на ту же страницу истории не копит', async () => {
    // Иначе десять нажатий по своему же разделу дают десять шагов назад,
    // и «назад» перестаёт работать как выход.
    const { result } = renderHook(() => useRoute())
    const было = window.history.length
    act(() => go({ name: 'sources' }))
    expect(window.history.length).toBe(было)
    expect(result.current).toEqual({ name: 'sources' })
  })
})
