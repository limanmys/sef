import { IProvider } from './provider'
import { ITool } from './tool'
import { IUser } from './user'
import { IDocument } from './document'

export interface IChatbot {
  id: number
  name: string
  description: string
  provider_id: number
  provider?: IProvider
  user_id: number
  user?: IUser
  model_name: string
  system_prompt?: string
  web_search_enabled?: boolean
  tool_format?: string
  output_format?: string
  // Model Parameters
  temperature?: number | null
  top_p?: number | null
  top_k?: number | null
  // Relationships
  tools?: ITool[]
  documents?: IDocument[]
  prompt_suggestions?: string[]
  created_at?: string
  updated_at?: string
}
