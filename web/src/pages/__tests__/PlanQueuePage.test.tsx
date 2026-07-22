import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { PlanQueuePage } from "../PlanQueuePage"

vi.mock("../../hooks/usePlan",()=>({usePlanQueue:()=>({data:[{plan_id:"pl-1",task_id:"task-1",name:"",title:"Search plan",summary:"Add FTS",status:"proposed",revision:2,unanswered_required:1,updated_at:""}],loading:false,error:null,refresh:vi.fn()})}))

describe("PlanQueuePage",()=>{it("links proposed plans and shows unresolved choices",()=>{render(<MemoryRouter><PlanQueuePage/></MemoryRouter>);expect(screen.getByRole("heading",{name:"Search plan"})).toBeTruthy();expect(screen.getByText("1 required")).toBeTruthy();expect(screen.getByRole("link").getAttribute("href")).toBe("/plans/pl-1")})})
