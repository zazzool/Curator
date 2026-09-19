import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// Разобранное дерево убирается между проверками: иначе вторая проверка
// находит узел, нарисованный первой, и падает там, где всё исправно.
afterEach(cleanup)
