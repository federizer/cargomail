import { createContext, ReactNode, useReducer } from 'react'
import type { Draft } from '../api/generated/proto/fedemail/v1/fedemail'
import useActionCreator from '../utils/hooks/action_creator'
import { IContact } from './ContactsContext'

const actions = {
  listDrafts: 'LIST_DRAFTS',
  createDraft: 'CREATE_DRAFT',
  deleteDraft: 'DELETE_DRAFT',
  newDraftEdit: 'NEW_DRAFT_EDIT',
  updateDraftEdit: 'UPDATE_DRAFT_EDIT',
  closeDraftEdit: 'CLOSE_DRAFT_EDIT',
}

const reducer = (state: any, action: any) => {
  const { payload } = action
  switch (action.type) {
    case actions.listDrafts:
      return {
        ...state,
        drafts: payload,
      }
    case actions.createDraft:
      return {
        ...state,
        drafts: [...state.drafts, payload],
      }
    case actions.deleteDraft: {
      return {
        ...state,
        drafts: state.drafts.filter((draft: { id: any }) => draft.id !== payload),
      }
    }
    case actions.newDraftEdit:
      return {
        ...state,
        editing: [...state.editing, payload],
      }
    case actions.updateDraftEdit:
      return {
        ...state,
        editing: state.editing.map((draft: { id: any }) => {
          if (draft.id === payload.id) {
            return { ...draft, ...payload }
          }
          return draft
        }),
      }
    case actions.closeDraftEdit: {
      return {
        ...state,
        editing: state.editing.filter((draft: { id: any }) => draft.id !== payload),
      }
    }
    default:
      return state
  }
}

export interface IDraftsProvider {
  children: ReactNode
}

export interface IDraftEdit {
  id: string
  sender: string
  recipients: IContact[]
  subject: string
  content: string
}

export interface IDraftsContext {
  draftsAll: { drafts: Draft[]; editing: IDraftEdit[] }
  listDrafts: (drafts: Draft[]) => void
  createDraft: (draft: Draft) => void
  deleteDraft: (id: string) => void
  newDraftEdit: (draft: IDraftEdit) => void
  updateDraftEdit: (draft: IDraftEdit) => void
  closeDraftEdit: (id: string) => void
}

export const DraftsContext = createContext<IDraftsContext>({
  draftsAll: { drafts: [] as Draft[], editing: [] as IDraftEdit[] },
  listDrafts: () => null,
  createDraft: () => null,
  deleteDraft: () => null,
  newDraftEdit: () => null,
  updateDraftEdit: () => null,
  closeDraftEdit: () => null,
})

export const DraftsProvider = (props: any) => {
  const [draftsAll, dispatch] = useReducer(reducer, {
    drafts: [] as Draft[],
    editing: [] as IDraftEdit[],
  })

  const listDrafts = useActionCreator(actions.listDrafts, dispatch)
  const createDraft = useActionCreator(actions.createDraft, dispatch)
  const deleteDraft = useActionCreator(actions.deleteDraft, dispatch)
  const newDraftEdit = useActionCreator(actions.newDraftEdit, dispatch)
  const updateDraftEdit = useActionCreator(actions.updateDraftEdit, dispatch)
  const closeDraftEdit = useActionCreator(actions.closeDraftEdit, dispatch)

  return (
    <DraftsContext.Provider
      value={{ draftsAll, listDrafts, createDraft, deleteDraft, newDraftEdit, updateDraftEdit, closeDraftEdit }}>
      {props.children}
    </DraftsContext.Provider>
  )
}
